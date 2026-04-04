package config

import (
	"strings"
	"testing"
	"time"

	"contractpro/document-management/internal/domain/model"
)

// setRequiredEnv sets all required environment variables with test values.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DM_DB_DSN", "postgres://dm:pass@localhost:5432/dm?sslmode=disable")
	t.Setenv("DM_BROKER_ADDRESS", "localhost:5672")
	t.Setenv("DM_STORAGE_ENDPOINT", "https://storage.yandexcloud.net")
	t.Setenv("DM_STORAGE_BUCKET", "dm-artifacts")
	t.Setenv("DM_STORAGE_ACCESS_KEY", "test-access-key")
	t.Setenv("DM_STORAGE_SECRET_KEY", "test-secret-key")
	t.Setenv("DM_KVSTORE_ADDRESS", "localhost:6379")
}

// --- Validation tests ---

func TestLoad_AllRequiredPresent(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Database.DSN != "postgres://dm:pass@localhost:5432/dm?sslmode=disable" {
		t.Errorf("Database.DSN = %q, want %q", cfg.Database.DSN, "postgres://dm:pass@localhost:5432/dm?sslmode=disable")
	}
	if cfg.Broker.Address != "localhost:5672" {
		t.Errorf("Broker.Address = %q, want %q", cfg.Broker.Address, "localhost:5672")
	}
	if cfg.Storage.Endpoint != "https://storage.yandexcloud.net" {
		t.Errorf("Storage.Endpoint = %q, want %q", cfg.Storage.Endpoint, "https://storage.yandexcloud.net")
	}
	if cfg.KVStore.Address != "localhost:6379" {
		t.Errorf("KVStore.Address = %q, want %q", cfg.KVStore.Address, "localhost:6379")
	}
}

func TestLoad_MissingSingleRequiredField(t *testing.T) {
	requiredFields := []struct {
		envVar string
		setFn  func(t *testing.T)
	}{
		{"DM_DB_DSN", func(t *testing.T) { t.Setenv("DM_DB_DSN", "") }},
		{"DM_BROKER_ADDRESS", func(t *testing.T) { t.Setenv("DM_BROKER_ADDRESS", "") }},
		{"DM_STORAGE_ENDPOINT", func(t *testing.T) { t.Setenv("DM_STORAGE_ENDPOINT", "") }},
		{"DM_STORAGE_BUCKET", func(t *testing.T) { t.Setenv("DM_STORAGE_BUCKET", "") }},
		{"DM_STORAGE_ACCESS_KEY", func(t *testing.T) { t.Setenv("DM_STORAGE_ACCESS_KEY", "") }},
		{"DM_STORAGE_SECRET_KEY", func(t *testing.T) { t.Setenv("DM_STORAGE_SECRET_KEY", "") }},
		{"DM_KVSTORE_ADDRESS", func(t *testing.T) { t.Setenv("DM_KVSTORE_ADDRESS", "") }},
	}

	for _, tc := range requiredFields {
		t.Run(tc.envVar, func(t *testing.T) {
			setRequiredEnv(t)
			tc.setFn(t)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error for missing %s, got nil", tc.envVar)
			}
			if !strings.Contains(err.Error(), tc.envVar) {
				t.Errorf("error %q should mention %s", err.Error(), tc.envVar)
			}
		})
	}
}

func TestLoad_MissingMultipleRequiredFields(t *testing.T) {
	// Explicitly clear all required env vars to guard against ambient environment.
	t.Setenv("DM_DB_DSN", "")
	t.Setenv("DM_BROKER_ADDRESS", "")
	t.Setenv("DM_STORAGE_ENDPOINT", "")
	t.Setenv("DM_STORAGE_BUCKET", "")
	t.Setenv("DM_STORAGE_ACCESS_KEY", "")
	t.Setenv("DM_STORAGE_SECRET_KEY", "")
	t.Setenv("DM_KVSTORE_ADDRESS", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	expectedFields := []string{
		"DM_DB_DSN",
		"DM_BROKER_ADDRESS",
		"DM_STORAGE_ENDPOINT",
		"DM_STORAGE_BUCKET",
		"DM_STORAGE_ACCESS_KEY",
		"DM_STORAGE_SECRET_KEY",
		"DM_KVSTORE_ADDRESS",
	}
	errMsg := err.Error()
	for _, field := range expectedFields {
		if !strings.Contains(errMsg, field) {
			t.Errorf("error should mention %s, got: %s", field, errMsg)
		}
	}
}

// --- Default values tests ---

func TestLoad_DefaultValues(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		// Database
		{"Database.MaxConns", cfg.Database.MaxConns, 25},
		{"Database.MinConns", cfg.Database.MinConns, 5},
		{"Database.QueryTimeout", cfg.Database.QueryTimeout, 10 * time.Second},
		// Broker
		{"Broker.TLS", cfg.Broker.TLS, false},
		// Storage
		{"Storage.Region", cfg.Storage.Region, "ru-central1"},
		{"Storage.PresignedURLTTL", cfg.Storage.PresignedURLTTL, 5 * time.Minute},
		// KVStore
		{"KVStore.Password", cfg.KVStore.Password, ""},
		{"KVStore.DB", cfg.KVStore.DB, 0},
		{"KVStore.PoolSize", cfg.KVStore.PoolSize, 10},
		{"KVStore.Timeout", cfg.KVStore.Timeout, 2 * time.Second},
		// HTTP
		{"HTTP.Port", cfg.HTTP.Port, 8080},
		// Consumer
		{"Consumer.Prefetch", cfg.Consumer.Prefetch, 10},
		{"Consumer.Concurrency", cfg.Consumer.Concurrency, 5},
		// Idempotency
		{"Idempotency.TTL", cfg.Idempotency.TTL, 24 * time.Hour},
		{"Idempotency.ProcessingTTL", cfg.Idempotency.ProcessingTTL, 120 * time.Second},
		{"Idempotency.StuckThreshold", cfg.Idempotency.StuckThreshold, 240 * time.Second},
		// Outbox
		{"Outbox.PollInterval", cfg.Outbox.PollInterval, 200 * time.Millisecond},
		{"Outbox.BatchSize", cfg.Outbox.BatchSize, 50},
		{"Outbox.LockTimeout", cfg.Outbox.LockTimeout, 5 * time.Second},
		{"Outbox.CleanupHours", cfg.Outbox.CleanupHours, 48},
		// Retention
		{"Retention.ArchiveDays", cfg.Retention.ArchiveDays, 90},
		{"Retention.DeletedBlobDays", cfg.Retention.DeletedBlobDays, 30},
		{"Retention.DeletedMetaDays", cfg.Retention.DeletedMetaDays, 365},
		{"Retention.AuditDays", cfg.Retention.AuditDays, 1095},
		// Retry
		{"Retry.MaxAttempts", cfg.Retry.MaxAttempts, 3},
		{"Retry.BackoffBase", cfg.Retry.BackoffBase, 1 * time.Second},
		// DLQ
		{"DLQ.MaxReplayCount", cfg.DLQ.MaxReplayCount, 3},
		// Observability
		{"Observability.LogLevel", cfg.Observability.LogLevel, "info"},
		{"Observability.MetricsPort", cfg.Observability.MetricsPort, 9090},
		{"Observability.TracingEnabled", cfg.Observability.TracingEnabled, false},
		{"Observability.TracingEndpoint", cfg.Observability.TracingEndpoint, ""},
		// Timeout
		{"Timeout.StoragePut", cfg.Timeout.StoragePut, 30 * time.Second},
		{"Timeout.StorageGet", cfg.Timeout.StorageGet, 15 * time.Second},
		{"Timeout.EventProcessing", cfg.Timeout.EventProcessing, 60 * time.Second},
		{"Timeout.BrokerPublish", cfg.Timeout.BrokerPublish, 10 * time.Second},
		{"Timeout.StaleVersion", cfg.Timeout.StaleVersion, 30 * time.Minute},
		{"Timeout.Shutdown", cfg.Timeout.Shutdown, 30 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
			}
		})
	}
}

// --- Topic defaults tests ---

func TestLoad_TopicDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	topics := []struct {
		name string
		got  string
		want string
	}{
		// Incoming topics — cross-verified against model.Topic* constants.
		{"TopicDPArtifactsProcessingReady", cfg.Broker.TopicDPArtifactsProcessingReady, model.TopicDPArtifactsProcessingReady},
		{"TopicDPRequestsSemanticTree", cfg.Broker.TopicDPRequestsSemanticTree, model.TopicDPRequestsSemanticTree},
		{"TopicDPArtifactsDiffReady", cfg.Broker.TopicDPArtifactsDiffReady, model.TopicDPArtifactsDiffReady},
		{"TopicLICArtifactsAnalysisReady", cfg.Broker.TopicLICArtifactsAnalysisReady, model.TopicLICArtifactsAnalysisReady},
		{"TopicLICRequestsArtifacts", cfg.Broker.TopicLICRequestsArtifacts, model.TopicLICRequestsArtifacts},
		{"TopicREArtifactsReportsReady", cfg.Broker.TopicREArtifactsReportsReady, model.TopicREArtifactsReportsReady},
		{"TopicRERequestsArtifacts", cfg.Broker.TopicRERequestsArtifacts, model.TopicRERequestsArtifacts},
		// Outgoing confirmation topics
		{"TopicDMResponsesArtifactsPersisted", cfg.Broker.TopicDMResponsesArtifactsPersisted, model.TopicDMResponsesArtifactsPersisted},
		{"TopicDMResponsesArtifactsPersistFailed", cfg.Broker.TopicDMResponsesArtifactsPersistFailed, model.TopicDMResponsesArtifactsPersistFailed},
		{"TopicDMResponsesSemanticTreeProvided", cfg.Broker.TopicDMResponsesSemanticTreeProvided, model.TopicDMResponsesSemanticTreeProvided},
		{"TopicDMResponsesArtifactsProvided", cfg.Broker.TopicDMResponsesArtifactsProvided, model.TopicDMResponsesArtifactsProvided},
		{"TopicDMResponsesDiffPersisted", cfg.Broker.TopicDMResponsesDiffPersisted, model.TopicDMResponsesDiffPersisted},
		{"TopicDMResponsesDiffPersistFailed", cfg.Broker.TopicDMResponsesDiffPersistFailed, model.TopicDMResponsesDiffPersistFailed},
		{"TopicDMResponsesLICArtifactsPersisted", cfg.Broker.TopicDMResponsesLICArtifactsPersisted, model.TopicDMResponsesLICArtifactsPersisted},
		{"TopicDMResponsesLICArtifactsPersistFailed", cfg.Broker.TopicDMResponsesLICArtifactsPersistFailed, model.TopicDMResponsesLICArtifactsPersistFailed},
		{"TopicDMResponsesREReportsPersisted", cfg.Broker.TopicDMResponsesREReportsPersisted, model.TopicDMResponsesREReportsPersisted},
		{"TopicDMResponsesREReportsPersistFailed", cfg.Broker.TopicDMResponsesREReportsPersistFailed, model.TopicDMResponsesREReportsPersistFailed},
		// Outgoing notification topics
		{"TopicDMEventsVersionArtifactsReady", cfg.Broker.TopicDMEventsVersionArtifactsReady, model.TopicDMEventsVersionArtifactsReady},
		{"TopicDMEventsVersionAnalysisReady", cfg.Broker.TopicDMEventsVersionAnalysisReady, model.TopicDMEventsVersionAnalysisReady},
		{"TopicDMEventsVersionReportsReady", cfg.Broker.TopicDMEventsVersionReportsReady, model.TopicDMEventsVersionReportsReady},
		{"TopicDMEventsVersionCreated", cfg.Broker.TopicDMEventsVersionCreated, model.TopicDMEventsVersionCreated},
		{"TopicDMEventsVersionPartiallyAvailable", cfg.Broker.TopicDMEventsVersionPartiallyAvailable, model.TopicDMEventsVersionPartiallyAvailable},
		// DLQ topics
		{"TopicDMDLQIngestionFailed", cfg.Broker.TopicDMDLQIngestionFailed, model.TopicDMDLQIngestionFailed},
		{"TopicDMDLQQueryFailed", cfg.Broker.TopicDMDLQQueryFailed, model.TopicDMDLQQueryFailed},
		{"TopicDMDLQInvalidMessage", cfg.Broker.TopicDMDLQInvalidMessage, model.TopicDMDLQInvalidMessage},
	}

	for _, tc := range topics {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// --- Override tests ---

func TestLoad_OverrideValues(t *testing.T) {
	setRequiredEnv(t)

	// Database overrides
	t.Setenv("DM_DB_MAX_CONNS", "50")
	t.Setenv("DM_DB_MIN_CONNS", "10")
	t.Setenv("DM_DB_QUERY_TIMEOUT", "30s")
	// Broker overrides
	t.Setenv("DM_BROKER_TLS", "true")
	// Storage overrides
	t.Setenv("DM_STORAGE_REGION", "ru-central3")
	t.Setenv("DM_STORAGE_PRESIGNED_TTL", "15m")
	// KVStore overrides
	t.Setenv("DM_KVSTORE_PASSWORD", "secret")
	t.Setenv("DM_KVSTORE_DB", "2")
	t.Setenv("DM_KVSTORE_POOL_SIZE", "20")
	t.Setenv("DM_KVSTORE_TIMEOUT", "10s")
	// HTTP overrides
	t.Setenv("DM_HTTP_PORT", "3000")
	// Consumer overrides
	t.Setenv("DM_CONSUMER_PREFETCH", "20")
	t.Setenv("DM_CONSUMER_CONCURRENCY", "10")
	// Idempotency overrides
	t.Setenv("DM_IDEMPOTENCY_TTL", "12h")
	t.Setenv("DM_IDEMPOTENCY_PROCESSING_TTL", "60s")
	t.Setenv("DM_IDEMPOTENCY_STUCK_THRESHOLD", "300s")
	// Outbox overrides
	t.Setenv("DM_OUTBOX_POLL_INTERVAL", "500ms")
	t.Setenv("DM_OUTBOX_BATCH_SIZE", "100")
	t.Setenv("DM_OUTBOX_LOCK_TIMEOUT", "10s")
	t.Setenv("DM_OUTBOX_CLEANUP_HOURS", "72")
	// Retention overrides
	t.Setenv("DM_RETENTION_ARCHIVE_DAYS", "180")
	t.Setenv("DM_RETENTION_DELETED_BLOB_DAYS", "60")
	t.Setenv("DM_RETENTION_DELETED_META_DAYS", "730")
	t.Setenv("DM_RETENTION_AUDIT_DAYS", "2190")
	// Retry overrides
	t.Setenv("DM_RETRY_MAX_ATTEMPTS", "5")
	t.Setenv("DM_RETRY_BACKOFF_BASE", "2s")
	// Observability overrides
	t.Setenv("DM_LOG_LEVEL", "debug")
	t.Setenv("DM_METRICS_PORT", "9191")
	t.Setenv("DM_TRACING_ENABLED", "true")
	t.Setenv("DM_TRACING_ENDPOINT", "http://jaeger:14268")
	// Timeout overrides
	t.Setenv("DM_TIMEOUT_STORAGE_PUT", "60s")
	t.Setenv("DM_TIMEOUT_STORAGE_GET", "30s")
	t.Setenv("DM_TIMEOUT_EVENT_PROCESSING", "120s")
	t.Setenv("DM_TIMEOUT_BROKER_PUBLISH", "20s")
	t.Setenv("DM_STALE_VERSION_TIMEOUT", "1h")
	t.Setenv("DM_SHUTDOWN_TIMEOUT", "60s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Database
	if cfg.Database.MaxConns != 50 {
		t.Errorf("Database.MaxConns = %d, want 50", cfg.Database.MaxConns)
	}
	if cfg.Database.MinConns != 10 {
		t.Errorf("Database.MinConns = %d, want 10", cfg.Database.MinConns)
	}
	if cfg.Database.QueryTimeout != 30*time.Second {
		t.Errorf("Database.QueryTimeout = %v, want 30s", cfg.Database.QueryTimeout)
	}
	// Broker
	if !cfg.Broker.TLS {
		t.Error("Broker.TLS should be true")
	}
	// Storage
	if cfg.Storage.Region != "ru-central3" {
		t.Errorf("Storage.Region = %q, want %q", cfg.Storage.Region, "ru-central3")
	}
	if cfg.Storage.PresignedURLTTL != 15*time.Minute {
		t.Errorf("Storage.PresignedURLTTL = %v, want 15m", cfg.Storage.PresignedURLTTL)
	}
	// KVStore
	if cfg.KVStore.Password != "secret" {
		t.Errorf("KVStore.Password = %q, want %q", cfg.KVStore.Password, "secret")
	}
	if cfg.KVStore.DB != 2 {
		t.Errorf("KVStore.DB = %d, want 2", cfg.KVStore.DB)
	}
	if cfg.KVStore.PoolSize != 20 {
		t.Errorf("KVStore.PoolSize = %d, want 20", cfg.KVStore.PoolSize)
	}
	if cfg.KVStore.Timeout != 10*time.Second {
		t.Errorf("KVStore.Timeout = %v, want 10s", cfg.KVStore.Timeout)
	}
	// HTTP
	if cfg.HTTP.Port != 3000 {
		t.Errorf("HTTP.Port = %d, want 3000", cfg.HTTP.Port)
	}
	// Consumer
	if cfg.Consumer.Prefetch != 20 {
		t.Errorf("Consumer.Prefetch = %d, want 20", cfg.Consumer.Prefetch)
	}
	if cfg.Consumer.Concurrency != 10 {
		t.Errorf("Consumer.Concurrency = %d, want 10", cfg.Consumer.Concurrency)
	}
	// Idempotency
	if cfg.Idempotency.TTL != 12*time.Hour {
		t.Errorf("Idempotency.TTL = %v, want 12h", cfg.Idempotency.TTL)
	}
	if cfg.Idempotency.ProcessingTTL != 60*time.Second {
		t.Errorf("Idempotency.ProcessingTTL = %v, want 60s", cfg.Idempotency.ProcessingTTL)
	}
	if cfg.Idempotency.StuckThreshold != 300*time.Second {
		t.Errorf("Idempotency.StuckThreshold = %v, want 300s", cfg.Idempotency.StuckThreshold)
	}
	// Outbox
	if cfg.Outbox.PollInterval != 500*time.Millisecond {
		t.Errorf("Outbox.PollInterval = %v, want 500ms", cfg.Outbox.PollInterval)
	}
	if cfg.Outbox.BatchSize != 100 {
		t.Errorf("Outbox.BatchSize = %d, want 100", cfg.Outbox.BatchSize)
	}
	if cfg.Outbox.LockTimeout != 10*time.Second {
		t.Errorf("Outbox.LockTimeout = %v, want 10s", cfg.Outbox.LockTimeout)
	}
	if cfg.Outbox.CleanupHours != 72 {
		t.Errorf("Outbox.CleanupHours = %d, want 72", cfg.Outbox.CleanupHours)
	}
	// Retention
	if cfg.Retention.ArchiveDays != 180 {
		t.Errorf("Retention.ArchiveDays = %d, want 180", cfg.Retention.ArchiveDays)
	}
	if cfg.Retention.DeletedBlobDays != 60 {
		t.Errorf("Retention.DeletedBlobDays = %d, want 60", cfg.Retention.DeletedBlobDays)
	}
	if cfg.Retention.DeletedMetaDays != 730 {
		t.Errorf("Retention.DeletedMetaDays = %d, want 730", cfg.Retention.DeletedMetaDays)
	}
	if cfg.Retention.AuditDays != 2190 {
		t.Errorf("Retention.AuditDays = %d, want 2190", cfg.Retention.AuditDays)
	}
	// Retry
	if cfg.Retry.MaxAttempts != 5 {
		t.Errorf("Retry.MaxAttempts = %d, want 5", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.BackoffBase != 2*time.Second {
		t.Errorf("Retry.BackoffBase = %v, want 2s", cfg.Retry.BackoffBase)
	}
	// Observability
	if cfg.Observability.LogLevel != "debug" {
		t.Errorf("Observability.LogLevel = %q, want %q", cfg.Observability.LogLevel, "debug")
	}
	if cfg.Observability.MetricsPort != 9191 {
		t.Errorf("Observability.MetricsPort = %d, want 9191", cfg.Observability.MetricsPort)
	}
	if !cfg.Observability.TracingEnabled {
		t.Error("Observability.TracingEnabled should be true")
	}
	if cfg.Observability.TracingEndpoint != "http://jaeger:14268" {
		t.Errorf("Observability.TracingEndpoint = %q, want %q", cfg.Observability.TracingEndpoint, "http://jaeger:14268")
	}
	// Timeout
	if cfg.Timeout.StoragePut != 60*time.Second {
		t.Errorf("Timeout.StoragePut = %v, want 60s", cfg.Timeout.StoragePut)
	}
	if cfg.Timeout.StorageGet != 30*time.Second {
		t.Errorf("Timeout.StorageGet = %v, want 30s", cfg.Timeout.StorageGet)
	}
	if cfg.Timeout.EventProcessing != 120*time.Second {
		t.Errorf("Timeout.EventProcessing = %v, want 120s", cfg.Timeout.EventProcessing)
	}
	if cfg.Timeout.BrokerPublish != 20*time.Second {
		t.Errorf("Timeout.BrokerPublish = %v, want 20s", cfg.Timeout.BrokerPublish)
	}
	if cfg.Timeout.StaleVersion != 1*time.Hour {
		t.Errorf("Timeout.StaleVersion = %v, want 1h", cfg.Timeout.StaleVersion)
	}
	if cfg.Timeout.Shutdown != 60*time.Second {
		t.Errorf("Timeout.Shutdown = %v, want 60s", cfg.Timeout.Shutdown)
	}
}

func TestLoad_OverrideTopics(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DM_BROKER_TOPIC_DP_ARTIFACTS_PROCESSING_READY", "custom.dp.artifacts.ready")
	t.Setenv("DM_BROKER_TOPIC_DM_DLQ_INGESTION_FAILED", "custom.dm.dlq.ingestion")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Broker.TopicDPArtifactsProcessingReady != "custom.dp.artifacts.ready" {
		t.Errorf("TopicDPArtifactsProcessingReady = %q, want %q", cfg.Broker.TopicDPArtifactsProcessingReady, "custom.dp.artifacts.ready")
	}
	if cfg.Broker.TopicDMDLQIngestionFailed != "custom.dm.dlq.ingestion" {
		t.Errorf("TopicDMDLQIngestionFailed = %q, want %q", cfg.Broker.TopicDMDLQIngestionFailed, "custom.dm.dlq.ingestion")
	}
	// Non-overridden topic should keep default.
	if cfg.Broker.TopicDPRequestsSemanticTree != "dp.requests.semantic-tree" {
		t.Errorf("TopicDPRequestsSemanticTree = %q, want default", cfg.Broker.TopicDPRequestsSemanticTree)
	}
}

// --- Env helper edge cases ---

func TestEnvHelpers_InvalidValues_FallbackToDefault(t *testing.T) {
	setRequiredEnv(t)

	t.Setenv("DM_DB_MAX_CONNS", "not-a-number")
	t.Setenv("DM_DB_MIN_CONNS", "abc")
	t.Setenv("DM_DB_QUERY_TIMEOUT", "invalid-duration")
	t.Setenv("DM_HTTP_PORT", "nope")
	t.Setenv("DM_CONSUMER_PREFETCH", "xyz")
	t.Setenv("DM_OUTBOX_BATCH_SIZE", "!!")
	t.Setenv("DM_RETENTION_ARCHIVE_DAYS", "bad")
	t.Setenv("DM_RETRY_MAX_ATTEMPTS", "?")
	t.Setenv("DM_METRICS_PORT", "oops")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Database.MaxConns != 25 {
		t.Errorf("Database.MaxConns should fall back to default, got %d", cfg.Database.MaxConns)
	}
	if cfg.Database.MinConns != 5 {
		t.Errorf("Database.MinConns should fall back to default, got %d", cfg.Database.MinConns)
	}
	if cfg.Database.QueryTimeout != 10*time.Second {
		t.Errorf("Database.QueryTimeout should fall back to default, got %v", cfg.Database.QueryTimeout)
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("HTTP.Port should fall back to default, got %d", cfg.HTTP.Port)
	}
	if cfg.Consumer.Prefetch != 10 {
		t.Errorf("Consumer.Prefetch should fall back to default, got %d", cfg.Consumer.Prefetch)
	}
	if cfg.Outbox.BatchSize != 50 {
		t.Errorf("Outbox.BatchSize should fall back to default, got %d", cfg.Outbox.BatchSize)
	}
	if cfg.Retention.ArchiveDays != 90 {
		t.Errorf("Retention.ArchiveDays should fall back to default, got %d", cfg.Retention.ArchiveDays)
	}
	if cfg.Retry.MaxAttempts != 3 {
		t.Errorf("Retry.MaxAttempts should fall back to default, got %d", cfg.Retry.MaxAttempts)
	}
	if cfg.Observability.MetricsPort != 9090 {
		t.Errorf("Observability.MetricsPort should fall back to default, got %d", cfg.Observability.MetricsPort)
	}
}

// --- Validate method directly ---

func TestValidate_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty config")
	}

	errMsg := err.Error()
	if !strings.HasPrefix(errMsg, "config: missing required: ") {
		t.Errorf("unexpected error format: %s", errMsg)
	}
}

func TestValidate_PartialConfig(t *testing.T) {
	cfg := &Config{
		Database:      DatabaseConfig{DSN: "postgres://localhost/dm"},
		Broker:        BrokerConfig{Address: "localhost:5672"},
		HTTP:          HTTPConfig{Port: 8080},
		Observability: ObservabilityConfig{MetricsPort: 9090},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing storage and kvstore fields")
	}

	errMsg := err.Error()
	// Should report all 4 missing storage fields + KVStore.
	storageFields := []string{"DM_STORAGE_ENDPOINT", "DM_STORAGE_BUCKET", "DM_STORAGE_ACCESS_KEY", "DM_STORAGE_SECRET_KEY"}
	for _, f := range storageFields {
		if !strings.Contains(errMsg, f) {
			t.Errorf("error should mention %s, got: %s", f, errMsg)
		}
	}
	if !strings.Contains(errMsg, "DM_KVSTORE_ADDRESS") {
		t.Errorf("error should mention DM_KVSTORE_ADDRESS, got: %s", errMsg)
	}
	// Should NOT report database or broker fields.
	if strings.Contains(errMsg, "DM_DB_DSN") {
		t.Error("error should not mention DM_DB_DSN (it is set)")
	}
	if strings.Contains(errMsg, "DM_BROKER_ADDRESS") {
		t.Error("error should not mention DM_BROKER_ADDRESS (it is set)")
	}
}

func TestValidate_FullConfig(t *testing.T) {
	cfg := &Config{
		Database:      DatabaseConfig{DSN: "postgres://localhost/dm"},
		Broker:        BrokerConfig{Address: "localhost:5672"},
		Storage:       StorageConfig{Endpoint: "e", Bucket: "b", AccessKey: "ak", SecretKey: "sk"},
		KVStore:       KVStoreConfig{Address: "localhost:6379"},
		HTTP:          HTTPConfig{Port: 8080},
		Consumer:      ConsumerConfig{Prefetch: 10, Concurrency: 5},
		Observability: ObservabilityConfig{MetricsPort: 9090},
		CircuitBreaker: CircuitBreakerConfig{
			MaxRequests:      3,
			Interval:         60 * time.Second,
			Timeout:          30 * time.Second,
			FailureThreshold: 5,
			PerEventBudget:   35 * time.Second,
		},
		Ingestion: IngestionConfig{
			MaxJSONArtifactBytes: 10 * 1024 * 1024,
			MaxBlobSizeBytes:     100 * 1024 * 1024,
		},
		OrphanCleanup: OrphanCleanupConfig{
			ScanInterval: 1 * time.Hour,
			BatchSize:    100,
			GracePeriod:  1 * time.Hour,
			ScanTimeout:  120 * time.Second,
		},
		Retention: RetentionConfig{
			ArchiveDays:       90,
			DeletedBlobDays:   30,
			DeletedMetaDays:   365,
			AuditDays:         1095,
			BlobScanInterval:  6 * time.Hour,
			MetaScanInterval:  24 * time.Hour,
			AuditScanInterval: 24 * time.Hour,
			BatchSize:         50,
			ScanTimeout:       300 * time.Second,
			AuditMonthsAhead:  3,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

// --- Port collision test ---

func TestValidate_PortCollision(t *testing.T) {
	cfg := &Config{
		Database:      DatabaseConfig{DSN: "postgres://localhost/dm"},
		Broker:        BrokerConfig{Address: "localhost:5672"},
		Storage:       StorageConfig{Endpoint: "e", Bucket: "b", AccessKey: "ak", SecretKey: "sk"},
		KVStore:       KVStoreConfig{Address: "localhost:6379"},
		HTTP:          HTTPConfig{Port: 8080},
		Consumer:      ConsumerConfig{Prefetch: 10, Concurrency: 5},
		Observability: ObservabilityConfig{MetricsPort: 8080}, // same as HTTP
		CircuitBreaker: CircuitBreakerConfig{
			MaxRequests:      3,
			Timeout:          30 * time.Second,
			FailureThreshold: 5,
			PerEventBudget:   35 * time.Second,
		},
		Ingestion:     IngestionConfig{MaxJSONArtifactBytes: 10 * 1024 * 1024, MaxBlobSizeBytes: 100 * 1024 * 1024},
		OrphanCleanup: OrphanCleanupConfig{ScanInterval: time.Hour, BatchSize: 100, GracePeriod: time.Hour, ScanTimeout: 120 * time.Second},
		Retention:     RetentionConfig{BlobScanInterval: 6 * time.Hour, MetaScanInterval: 24 * time.Hour, AuditScanInterval: 24 * time.Hour, BatchSize: 50, ScanTimeout: 300 * time.Second, AuditMonthsAhead: 3},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for port collision, got nil")
	}
	if !strings.Contains(err.Error(), "DM_HTTP_PORT and DM_METRICS_PORT must differ") || !strings.Contains(err.Error(), "invalid:") {
		t.Errorf("error should mention port collision, got: %s", err.Error())
	}
}

func TestLoad_PortCollision_SamePort(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DM_HTTP_PORT", "9090")
	// DM_METRICS_PORT defaults to 9090

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for port collision, got nil")
	}
	if !strings.Contains(err.Error(), "DM_HTTP_PORT and DM_METRICS_PORT must differ") || !strings.Contains(err.Error(), "invalid:") {
		t.Errorf("error should mention port collision, got: %s", err.Error())
	}
}

// --- Consumer config validation tests (BRE-007) ---

func TestValidate_ConsumerConcurrencyZero(t *testing.T) {
	cfg := &Config{
		Database:      DatabaseConfig{DSN: "postgres://localhost/dm"},
		Broker:        BrokerConfig{Address: "localhost:5672"},
		Storage:       StorageConfig{Endpoint: "e", Bucket: "b", AccessKey: "ak", SecretKey: "sk"},
		KVStore:       KVStoreConfig{Address: "localhost:6379"},
		HTTP:          HTTPConfig{Port: 8080},
		Consumer:      ConsumerConfig{Prefetch: 10, Concurrency: 0},
		Observability: ObservabilityConfig{MetricsPort: 9090},
		CircuitBreaker: CircuitBreakerConfig{
			MaxRequests: 3, Timeout: 30 * time.Second,
			FailureThreshold: 5, PerEventBudget: 35 * time.Second,
		},
		Ingestion: IngestionConfig{MaxJSONArtifactBytes: 10 * 1024 * 1024, MaxBlobSizeBytes: 100 * 1024 * 1024},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero concurrency")
	}
	if !strings.Contains(err.Error(), "DM_CONSUMER_CONCURRENCY must be >= 1") {
		t.Errorf("expected concurrency error, got: %s", err.Error())
	}
}

func TestValidate_ConsumerPrefetchZero(t *testing.T) {
	cfg := &Config{
		Database:      DatabaseConfig{DSN: "postgres://localhost/dm"},
		Broker:        BrokerConfig{Address: "localhost:5672"},
		Storage:       StorageConfig{Endpoint: "e", Bucket: "b", AccessKey: "ak", SecretKey: "sk"},
		KVStore:       KVStoreConfig{Address: "localhost:6379"},
		HTTP:          HTTPConfig{Port: 8080},
		Consumer:      ConsumerConfig{Prefetch: 0, Concurrency: 5},
		Observability: ObservabilityConfig{MetricsPort: 9090},
		CircuitBreaker: CircuitBreakerConfig{
			MaxRequests: 3, Timeout: 30 * time.Second,
			FailureThreshold: 5, PerEventBudget: 35 * time.Second,
		},
		Ingestion: IngestionConfig{MaxJSONArtifactBytes: 10 * 1024 * 1024, MaxBlobSizeBytes: 100 * 1024 * 1024},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero prefetch")
	}
	if !strings.Contains(err.Error(), "DM_CONSUMER_PREFETCH must be >= 1") {
		t.Errorf("expected prefetch error, got: %s", err.Error())
	}
}

// --- envBool tests ---

func TestEnvBool_ValidValues(t *testing.T) {
	tests := []struct {
		name       string
		envValue   string
		defaultVal bool
		want       bool
	}{
		{"true", "true", false, true},
		{"TRUE", "TRUE", false, true},
		{"1", "1", false, true},
		{"false", "false", true, false},
		{"FALSE", "FALSE", true, false},
		{"0", "0", true, false},
		{"t", "t", false, true},
		{"f", "f", true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DM_TEST_BOOL", tc.envValue)
			got := envBool("DM_TEST_BOOL", tc.defaultVal)
			if got != tc.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", tc.envValue, tc.defaultVal, got, tc.want)
			}
		})
	}
}

func TestEnvBool_EmptyFallsBackToDefault(t *testing.T) {
	// Do not set DM_TEST_BOOL_EMPTY at all.
	got := envBool("DM_TEST_BOOL_EMPTY", true)
	if !got {
		t.Errorf("envBool with unset var should return default true, got false")
	}

	got = envBool("DM_TEST_BOOL_EMPTY", false)
	if got {
		t.Errorf("envBool with unset var should return default false, got true")
	}
}

func TestEnvBool_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("DM_TEST_BOOL_INVALID", "not-a-bool")

	got := envBool("DM_TEST_BOOL_INVALID", true)
	if !got {
		t.Errorf("envBool with invalid value should return default true, got false")
	}

	got = envBool("DM_TEST_BOOL_INVALID", false)
	if got {
		t.Errorf("envBool with invalid value should return default false, got true")
	}
}

func TestLoad_TracingEnabled_EnvBoolIntegration(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DM_TRACING_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Observability.TracingEnabled {
		t.Error("TracingEnabled should be true when DM_TRACING_ENABLED=true")
	}
}

func TestLoad_TracingEnabled_DefaultFalse(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Observability.TracingEnabled {
		t.Error("TracingEnabled should default to false")
	}
}

func TestLoad_BrokerTLS_DefaultFalse(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Broker.TLS {
		t.Error("Broker.TLS should default to false")
	}
}

func TestLoad_BrokerTLS_Override(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DM_BROKER_TLS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Broker.TLS {
		t.Error("Broker.TLS should be true when DM_BROKER_TLS=true")
	}
}

// --- Ingestion config validation tests (BRE-029) ---

func TestValidate_IngestionMaxJSONZero(t *testing.T) {
	cfg := &Config{
		Database:      DatabaseConfig{DSN: "postgres://localhost/dm"},
		Broker:        BrokerConfig{Address: "localhost:5672"},
		Storage:       StorageConfig{Endpoint: "e", Bucket: "b", AccessKey: "ak", SecretKey: "sk"},
		KVStore:       KVStoreConfig{Address: "localhost:6379"},
		HTTP:          HTTPConfig{Port: 8080},
		Consumer:      ConsumerConfig{Prefetch: 10, Concurrency: 5},
		Observability: ObservabilityConfig{MetricsPort: 9090},
		CircuitBreaker: CircuitBreakerConfig{
			MaxRequests: 3, Timeout: 30 * time.Second,
			FailureThreshold: 5, PerEventBudget: 35 * time.Second,
		},
		Ingestion: IngestionConfig{MaxJSONArtifactBytes: 0, MaxBlobSizeBytes: 100 * 1024 * 1024},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero MaxJSONArtifactBytes")
	}
	if !strings.Contains(err.Error(), "DM_INGESTION_MAX_JSON_BYTES must be positive") {
		t.Errorf("expected ingestion JSON bytes error, got: %s", err.Error())
	}
}

func TestValidate_IngestionMaxBlobNegative(t *testing.T) {
	cfg := &Config{
		Database:      DatabaseConfig{DSN: "postgres://localhost/dm"},
		Broker:        BrokerConfig{Address: "localhost:5672"},
		Storage:       StorageConfig{Endpoint: "e", Bucket: "b", AccessKey: "ak", SecretKey: "sk"},
		KVStore:       KVStoreConfig{Address: "localhost:6379"},
		HTTP:          HTTPConfig{Port: 8080},
		Consumer:      ConsumerConfig{Prefetch: 10, Concurrency: 5},
		Observability: ObservabilityConfig{MetricsPort: 9090},
		CircuitBreaker: CircuitBreakerConfig{
			MaxRequests: 3, Timeout: 30 * time.Second,
			FailureThreshold: 5, PerEventBudget: 35 * time.Second,
		},
		Ingestion: IngestionConfig{MaxJSONArtifactBytes: 10 * 1024 * 1024, MaxBlobSizeBytes: -1},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative MaxBlobSizeBytes")
	}
	if !strings.Contains(err.Error(), "DM_INGESTION_MAX_BLOB_SIZE_BYTES must be positive") {
		t.Errorf("expected ingestion blob bytes error, got: %s", err.Error())
	}
}

func TestLoad_IngestionDefaults(t *testing.T) {
	setRequiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingestion.MaxJSONArtifactBytes != 10*1024*1024 {
		t.Errorf("Ingestion.MaxJSONArtifactBytes = %d, want %d", cfg.Ingestion.MaxJSONArtifactBytes, 10*1024*1024)
	}
	if cfg.Ingestion.MaxBlobSizeBytes != 100*1024*1024 {
		t.Errorf("Ingestion.MaxBlobSizeBytes = %d, want %d", cfg.Ingestion.MaxBlobSizeBytes, 100*1024*1024)
	}
}

func TestLoad_IngestionOverrides(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DM_INGESTION_MAX_JSON_BYTES", "5242880")
	t.Setenv("DM_INGESTION_MAX_BLOB_SIZE_BYTES", "209715200")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingestion.MaxJSONArtifactBytes != 5242880 {
		t.Errorf("Ingestion.MaxJSONArtifactBytes = %d, want 5242880", cfg.Ingestion.MaxJSONArtifactBytes)
	}
	if cfg.Ingestion.MaxBlobSizeBytes != 209715200 {
		t.Errorf("Ingestion.MaxBlobSizeBytes = %d, want 209715200", cfg.Ingestion.MaxBlobSizeBytes)
	}
}

// --- HTTPPort is optional (has default) ---

func TestLoad_HTTPPort_DefaultWhenUnset(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("HTTP.Port = %d, want default 8080", cfg.HTTP.Port)
	}
}
