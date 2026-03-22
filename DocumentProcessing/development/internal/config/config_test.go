package config

import (
	"strings"
	"testing"
	"time"
)

// setRequiredEnv sets all required environment variables with test values.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DP_BROKER_ADDRESS", "localhost:9092")
	t.Setenv("DP_STORAGE_ENDPOINT", "https://storage.yandexcloud.net")
	t.Setenv("DP_STORAGE_BUCKET", "dp-artifacts")
	t.Setenv("DP_STORAGE_ACCESS_KEY", "test-access-key")
	t.Setenv("DP_STORAGE_SECRET_KEY", "test-secret-key")
	t.Setenv("DP_OCR_ENDPOINT", "https://ocr.api.cloud.yandex.net")
	t.Setenv("DP_OCR_API_KEY", "test-api-key")
	t.Setenv("DP_OCR_FOLDER_ID", "test-folder-id")
	t.Setenv("DP_KVSTORE_ADDRESS", "localhost:6379")
}

// --- Validation tests ---

func TestLoad_AllRequiredPresent(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Broker.Address != "localhost:9092" {
		t.Errorf("Broker.Address = %q, want %q", cfg.Broker.Address, "localhost:9092")
	}
	if cfg.Storage.Endpoint != "https://storage.yandexcloud.net" {
		t.Errorf("Storage.Endpoint = %q, want %q", cfg.Storage.Endpoint, "https://storage.yandexcloud.net")
	}
	if cfg.OCR.APIKey != "test-api-key" {
		t.Errorf("OCR.APIKey = %q, want %q", cfg.OCR.APIKey, "test-api-key")
	}
}

func TestLoad_MissingSingleRequiredField(t *testing.T) {
	requiredFields := []struct {
		envVar string
		setFn  func(t *testing.T)
	}{
		{"DP_BROKER_ADDRESS", func(t *testing.T) { t.Setenv("DP_BROKER_ADDRESS", "") }},
		{"DP_STORAGE_ENDPOINT", func(t *testing.T) { t.Setenv("DP_STORAGE_ENDPOINT", "") }},
		{"DP_STORAGE_BUCKET", func(t *testing.T) { t.Setenv("DP_STORAGE_BUCKET", "") }},
		{"DP_STORAGE_ACCESS_KEY", func(t *testing.T) { t.Setenv("DP_STORAGE_ACCESS_KEY", "") }},
		{"DP_STORAGE_SECRET_KEY", func(t *testing.T) { t.Setenv("DP_STORAGE_SECRET_KEY", "") }},
		{"DP_OCR_ENDPOINT", func(t *testing.T) { t.Setenv("DP_OCR_ENDPOINT", "") }},
		{"DP_OCR_API_KEY", func(t *testing.T) { t.Setenv("DP_OCR_API_KEY", "") }},
		{"DP_OCR_FOLDER_ID", func(t *testing.T) { t.Setenv("DP_OCR_FOLDER_ID", "") }},
		{"DP_KVSTORE_ADDRESS", func(t *testing.T) { t.Setenv("DP_KVSTORE_ADDRESS", "") }},
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
	// Don't set any required env vars — all should be reported.
	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	expectedFields := []string{
		"DP_BROKER_ADDRESS",
		"DP_STORAGE_ENDPOINT",
		"DP_STORAGE_BUCKET",
		"DP_STORAGE_ACCESS_KEY",
		"DP_STORAGE_SECRET_KEY",
		"DP_OCR_ENDPOINT",
		"DP_OCR_API_KEY",
		"DP_OCR_FOLDER_ID",
		"DP_KVSTORE_ADDRESS",
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
		name   string
		got    interface{}
		want   interface{}
	}{
		// Limits
		{"Limits.MaxFileSize", cfg.Limits.MaxFileSize, int64(20971520)},
		{"Limits.MaxPages", cfg.Limits.MaxPages, 100},
		{"Limits.JobTimeout", cfg.Limits.JobTimeout, 120 * time.Second},
		// Concurrency
		{"Concurrency.MaxConcurrentJobs", cfg.Concurrency.MaxConcurrentJobs, 5},
		// Idempotency
		{"Idempotency.TTL", cfg.Idempotency.TTL, 24 * time.Hour},
		// OCR
		{"OCR.RPSLimit", cfg.OCR.RPSLimit, 10},
		// Observability
		{"Observability.LogLevel", cfg.Observability.LogLevel, "info"},
		{"Observability.MetricsPort", cfg.Observability.MetricsPort, 9090},
		{"Observability.TracingEndpoint", cfg.Observability.TracingEndpoint, ""},
		// HTTP
		{"HTTP.Port", cfg.HTTP.Port, 8080},
		// Retry
		{"Retry.MaxAttempts", cfg.Retry.MaxAttempts, 3},
		{"Retry.BackoffBase", cfg.Retry.BackoffBase, 1 * time.Second},
		// KVStore
		{"KVStore.Password", cfg.KVStore.Password, ""},
		{"KVStore.DB", cfg.KVStore.DB, 0},
		{"KVStore.PoolSize", cfg.KVStore.PoolSize, 10},
		{"KVStore.Timeout", cfg.KVStore.Timeout, 5 * time.Second},
		// Storage
		{"Storage.Region", cfg.Storage.Region, "ru-central1"},
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
		// DP inbound commands
		{"TopicProcessDocument", cfg.Broker.TopicProcessDocument, "dp.commands.process-document"},
		{"TopicCompareVersions", cfg.Broker.TopicCompareVersions, "dp.commands.compare-versions"},
		// DP -> DM
		{"TopicArtifactsReady", cfg.Broker.TopicArtifactsReady, "dp.artifacts.processing-ready"},
		{"TopicSemanticTreeReq", cfg.Broker.TopicSemanticTreeReq, "dp.requests.semantic-tree"},
		{"TopicDiffReady", cfg.Broker.TopicDiffReady, "dp.artifacts.diff-ready"},
		// DM -> DP responses
		{"TopicDMArtifactsPersisted", cfg.Broker.TopicDMArtifactsPersisted, "dm.responses.artifacts-persisted"},
		{"TopicDMArtifactsPersistFailed", cfg.Broker.TopicDMArtifactsPersistFailed, "dm.responses.artifacts-persist-failed"},
		{"TopicDMSemanticTreeProvided", cfg.Broker.TopicDMSemanticTreeProvided, "dm.responses.semantic-tree-provided"},
		{"TopicDMDiffPersisted", cfg.Broker.TopicDMDiffPersisted, "dm.responses.diff-persisted"},
		{"TopicDMDiffPersistFailed", cfg.Broker.TopicDMDiffPersistFailed, "dm.responses.diff-persist-failed"},
		// DP -> external events
		{"TopicStatusChanged", cfg.Broker.TopicStatusChanged, "dp.events.status-changed"},
		{"TopicProcessingCompleted", cfg.Broker.TopicProcessingCompleted, "dp.events.processing-completed"},
		{"TopicProcessingFailed", cfg.Broker.TopicProcessingFailed, "dp.events.processing-failed"},
		{"TopicComparisonCompleted", cfg.Broker.TopicComparisonCompleted, "dp.events.comparison-completed"},
		{"TopicComparisonFailed", cfg.Broker.TopicComparisonFailed, "dp.events.comparison-failed"},
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

	t.Setenv("DP_LIMITS_MAX_FILE_SIZE", "10485760")
	t.Setenv("DP_LIMITS_MAX_PAGES", "50")
	t.Setenv("DP_LIMITS_JOB_TIMEOUT", "60s")
	t.Setenv("DP_CONCURRENCY_MAX_JOBS", "10")
	t.Setenv("DP_IDEMPOTENCY_TTL", "12h")
	t.Setenv("DP_OCR_RPS_LIMIT", "20")
	t.Setenv("DP_LOG_LEVEL", "debug")
	t.Setenv("DP_METRICS_PORT", "9191")
	t.Setenv("DP_TRACING_ENDPOINT", "http://jaeger:14268")
	t.Setenv("DP_HTTP_PORT", "3000")
	t.Setenv("DP_RETRY_MAX_ATTEMPTS", "5")
	t.Setenv("DP_RETRY_BACKOFF_BASE", "2s")
	t.Setenv("DP_STORAGE_REGION", "ru-central3")
	t.Setenv("DP_KVSTORE_ADDRESS", "redis.example.com:6380")
	t.Setenv("DP_KVSTORE_PASSWORD", "secret")
	t.Setenv("DP_KVSTORE_DB", "2")
	t.Setenv("DP_KVSTORE_POOL_SIZE", "20")
	t.Setenv("DP_KVSTORE_TIMEOUT", "10s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.KVStore.Address != "redis.example.com:6380" {
		t.Errorf("KVStore.Address = %q, want %q", cfg.KVStore.Address, "redis.example.com:6380")
	}
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
	if cfg.Limits.MaxFileSize != 10485760 {
		t.Errorf("Limits.MaxFileSize = %d, want 10485760", cfg.Limits.MaxFileSize)
	}
	if cfg.Limits.MaxPages != 50 {
		t.Errorf("Limits.MaxPages = %d, want 50", cfg.Limits.MaxPages)
	}
	if cfg.Limits.JobTimeout != 60*time.Second {
		t.Errorf("Limits.JobTimeout = %v, want 60s", cfg.Limits.JobTimeout)
	}
	if cfg.Concurrency.MaxConcurrentJobs != 10 {
		t.Errorf("Concurrency.MaxConcurrentJobs = %d, want 10", cfg.Concurrency.MaxConcurrentJobs)
	}
	if cfg.Idempotency.TTL != 12*time.Hour {
		t.Errorf("Idempotency.TTL = %v, want 12h", cfg.Idempotency.TTL)
	}
	if cfg.OCR.RPSLimit != 20 {
		t.Errorf("OCR.RPSLimit = %d, want 20", cfg.OCR.RPSLimit)
	}
	if cfg.Observability.LogLevel != "debug" {
		t.Errorf("Observability.LogLevel = %q, want %q", cfg.Observability.LogLevel, "debug")
	}
	if cfg.Observability.MetricsPort != 9191 {
		t.Errorf("Observability.MetricsPort = %d, want 9191", cfg.Observability.MetricsPort)
	}
	if cfg.Observability.TracingEndpoint != "http://jaeger:14268" {
		t.Errorf("Observability.TracingEndpoint = %q, want %q", cfg.Observability.TracingEndpoint, "http://jaeger:14268")
	}
	if cfg.HTTP.Port != 3000 {
		t.Errorf("HTTP.Port = %d, want 3000", cfg.HTTP.Port)
	}
	if cfg.Retry.MaxAttempts != 5 {
		t.Errorf("Retry.MaxAttempts = %d, want 5", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.BackoffBase != 2*time.Second {
		t.Errorf("Retry.BackoffBase = %v, want 2s", cfg.Retry.BackoffBase)
	}
	if cfg.Storage.Region != "ru-central3" {
		t.Errorf("Storage.Region = %q, want %q", cfg.Storage.Region, "ru-central3")
	}
}

func TestLoad_OverrideTopics(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DP_BROKER_TOPIC_PROCESS_DOCUMENT", "custom.commands.process")
	t.Setenv("DP_BROKER_TOPIC_STATUS_CHANGED", "custom.events.status")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Broker.TopicProcessDocument != "custom.commands.process" {
		t.Errorf("TopicProcessDocument = %q, want %q", cfg.Broker.TopicProcessDocument, "custom.commands.process")
	}
	if cfg.Broker.TopicStatusChanged != "custom.events.status" {
		t.Errorf("TopicStatusChanged = %q, want %q", cfg.Broker.TopicStatusChanged, "custom.events.status")
	}
	// Non-overridden topic should keep default.
	if cfg.Broker.TopicCompareVersions != "dp.commands.compare-versions" {
		t.Errorf("TopicCompareVersions = %q, want default", cfg.Broker.TopicCompareVersions)
	}
}

// --- Env helper edge cases ---

func TestEnvHelpers_InvalidValues_FallbackToDefault(t *testing.T) {
	setRequiredEnv(t)

	t.Setenv("DP_LIMITS_MAX_FILE_SIZE", "not-a-number")
	t.Setenv("DP_LIMITS_MAX_PAGES", "abc")
	t.Setenv("DP_LIMITS_JOB_TIMEOUT", "invalid-duration")
	t.Setenv("DP_OCR_RPS_LIMIT", "xyz")
	t.Setenv("DP_HTTP_PORT", "nope")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Limits.MaxFileSize != 20971520 {
		t.Errorf("MaxFileSize should fall back to default, got %d", cfg.Limits.MaxFileSize)
	}
	if cfg.Limits.MaxPages != 100 {
		t.Errorf("MaxPages should fall back to default, got %d", cfg.Limits.MaxPages)
	}
	if cfg.Limits.JobTimeout != 120*time.Second {
		t.Errorf("JobTimeout should fall back to default, got %v", cfg.Limits.JobTimeout)
	}
	if cfg.OCR.RPSLimit != 10 {
		t.Errorf("RPSLimit should fall back to default, got %d", cfg.OCR.RPSLimit)
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("HTTP.Port should fall back to default, got %d", cfg.HTTP.Port)
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
	if !strings.HasPrefix(errMsg, "config: missing required environment variables:") {
		t.Errorf("unexpected error format: %s", errMsg)
	}
}

func TestValidate_PartialConfig(t *testing.T) {
	cfg := &Config{
		Broker: BrokerConfig{Address: "localhost:9092"},
		OCR:    OCRConfig{Endpoint: "https://ocr", APIKey: "key", FolderID: "folder"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing storage fields")
	}

	errMsg := err.Error()
	// Should report all 4 missing storage fields.
	storageFields := []string{"DP_STORAGE_ENDPOINT", "DP_STORAGE_BUCKET", "DP_STORAGE_ACCESS_KEY", "DP_STORAGE_SECRET_KEY"}
	for _, f := range storageFields {
		if !strings.Contains(errMsg, f) {
			t.Errorf("error should mention %s, got: %s", f, errMsg)
		}
	}
	// Should NOT report broker or OCR fields.
	if strings.Contains(errMsg, "DP_BROKER_ADDRESS") {
		t.Error("error should not mention DP_BROKER_ADDRESS (it is set)")
	}
	if strings.Contains(errMsg, "DP_OCR_ENDPOINT") {
		t.Error("error should not mention DP_OCR_ENDPOINT (it is set)")
	}
}

func TestValidate_FullConfig(t *testing.T) {
	cfg := &Config{
		Broker:  BrokerConfig{Address: "localhost:9092"},
		Storage: StorageConfig{Endpoint: "e", Bucket: "b", AccessKey: "ak", SecretKey: "sk"},
		OCR:     OCRConfig{Endpoint: "e", APIKey: "k", FolderID: "f"},
		KVStore: KVStoreConfig{Address: "localhost:6379"},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}
