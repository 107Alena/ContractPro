package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createTempKeyFile creates a temporary JWT public key file for tests.
func createTempKeyFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt-public.pem")
	if err := os.WriteFile(path, []byte("fake-public-key"), 0o644); err != nil {
		t.Fatalf("failed to create temp key file: %v", err)
	}
	return path
}

// setRequiredEnv sets all required environment variables with test values.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ORCH_DM_BASE_URL", "http://dm:8080/api/v1")
	t.Setenv("ORCH_BROKER_ADDRESS", "amqp://guest:guest@localhost:5672/")
	t.Setenv("ORCH_STORAGE_ENDPOINT", "http://localhost:9000")
	t.Setenv("ORCH_STORAGE_BUCKET", "contractpro-uploads")
	t.Setenv("ORCH_STORAGE_ACCESS_KEY", "minioadmin")
	t.Setenv("ORCH_STORAGE_SECRET_KEY", "minioadmin")
	t.Setenv("ORCH_REDIS_ADDRESS", "localhost:6379")
	t.Setenv("ORCH_JWT_PUBLIC_KEY_PATH", createTempKeyFile(t))
}

// --- Happy path ---

func TestLoad_AllRequiredPresent(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.DMClient.BaseURL != "http://dm:8080/api/v1" {
		t.Errorf("DMClient.BaseURL = %q, want %q", cfg.DMClient.BaseURL, "http://dm:8080/api/v1")
	}
	if cfg.Broker.Address != "amqp://guest:guest@localhost:5672/" {
		t.Errorf("Broker.Address = %q, want %q", cfg.Broker.Address, "amqp://guest:guest@localhost:5672/")
	}
	if cfg.Storage.Endpoint != "http://localhost:9000" {
		t.Errorf("Storage.Endpoint = %q, want %q", cfg.Storage.Endpoint, "http://localhost:9000")
	}
	if cfg.Redis.Address != "localhost:6379" {
		t.Errorf("Redis.Address = %q, want %q", cfg.Redis.Address, "localhost:6379")
	}
}

// --- Missing required fields ---

func TestLoad_MissingSingleRequiredField(t *testing.T) {
	requiredFields := []struct {
		envVar string
	}{
		{"ORCH_DM_BASE_URL"},
		{"ORCH_BROKER_ADDRESS"},
		{"ORCH_STORAGE_ENDPOINT"},
		{"ORCH_STORAGE_BUCKET"},
		{"ORCH_STORAGE_ACCESS_KEY"},
		{"ORCH_STORAGE_SECRET_KEY"},
		{"ORCH_REDIS_ADDRESS"},
		{"ORCH_JWT_PUBLIC_KEY_PATH"},
	}

	for _, tc := range requiredFields {
		t.Run(tc.envVar, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(tc.envVar, "")

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
	// Explicitly clear all required env vars for deterministic test.
	t.Setenv("ORCH_DM_BASE_URL", "")
	t.Setenv("ORCH_BROKER_ADDRESS", "")
	t.Setenv("ORCH_STORAGE_ENDPOINT", "")
	t.Setenv("ORCH_STORAGE_BUCKET", "")
	t.Setenv("ORCH_STORAGE_ACCESS_KEY", "")
	t.Setenv("ORCH_STORAGE_SECRET_KEY", "")
	t.Setenv("ORCH_REDIS_ADDRESS", "")
	t.Setenv("ORCH_JWT_PUBLIC_KEY_PATH", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	expectedFields := []string{
		"ORCH_DM_BASE_URL",
		"ORCH_BROKER_ADDRESS",
		"ORCH_STORAGE_ENDPOINT",
		"ORCH_STORAGE_BUCKET",
		"ORCH_STORAGE_ACCESS_KEY",
		"ORCH_STORAGE_SECRET_KEY",
		"ORCH_REDIS_ADDRESS",
		"ORCH_JWT_PUBLIC_KEY_PATH",
	}
	errMsg := err.Error()
	for _, field := range expectedFields {
		if !strings.Contains(errMsg, field) {
			t.Errorf("error should mention %s, got: %s", field, errMsg)
		}
	}
}

// --- Default values ---

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
		// HTTP
		{"HTTP.Port", cfg.HTTP.Port, 8080},
		{"HTTP.MetricsPort", cfg.HTTP.MetricsPort, 9090},
		{"HTTP.RequestTimeout", cfg.HTTP.RequestTimeout, 30 * time.Second},
		{"HTTP.UploadTimeout", cfg.HTTP.UploadTimeout, 60 * time.Second},
		{"HTTP.ShutdownTimeout", cfg.HTTP.ShutdownTimeout, 30 * time.Second},
		// Broker
		{"Broker.TLS", cfg.Broker.TLS, false},
		{"Broker.Prefetch", cfg.Broker.Prefetch, 10},
		// Storage
		{"Storage.Region", cfg.Storage.Region, "ru-central1"},
		{"Storage.UploadTimeout", cfg.Storage.UploadTimeout, 30 * time.Second},
		// Redis
		{"Redis.Password", cfg.Redis.Password, ""},
		{"Redis.DB", cfg.Redis.DB, 0},
		{"Redis.PoolSize", cfg.Redis.PoolSize, 20},
		{"Redis.Timeout", cfg.Redis.Timeout, 2 * time.Second},
		// Upload
		{"Upload.MaxSize", cfg.Upload.MaxSize, int64(20971520)},
		{"Upload.MaxConcurrent", cfg.Upload.MaxConcurrent, 10},
		// DM Client
		{"DMClient.TimeoutRead", cfg.DMClient.TimeoutRead, 5 * time.Second},
		{"DMClient.TimeoutWrite", cfg.DMClient.TimeoutWrite, 10 * time.Second},
		{"DMClient.RetryMax", cfg.DMClient.RetryMax, 3},
		{"DMClient.RetryBackoff", cfg.DMClient.RetryBackoff, 200 * time.Millisecond},
		// OPM Client
		{"OPMClient.BaseURL", cfg.OPMClient.BaseURL, ""},
		{"OPMClient.Timeout", cfg.OPMClient.Timeout, 5 * time.Second},
		// UOM Client
		{"UOMClient.BaseURL", cfg.UOMClient.BaseURL, ""},
		{"UOMClient.Timeout", cfg.UOMClient.Timeout, 5 * time.Second},
		// SSE
		{"SSE.HeartbeatInterval", cfg.SSE.HeartbeatInterval, 15 * time.Second},
		{"SSE.MaxConnectionAge", cfg.SSE.MaxConnectionAge, 24 * time.Hour},
		// Rate Limit
		{"RateLimit.Enabled", cfg.RateLimit.Enabled, true},
		{"RateLimit.ReadRPS", cfg.RateLimit.ReadRPS, 200},
		{"RateLimit.WriteRPS", cfg.RateLimit.WriteRPS, 50},
		// Circuit Breaker
		{"CircuitBreaker.FailureThreshold", cfg.CircuitBreaker.FailureThreshold, 5},
		{"CircuitBreaker.Timeout", cfg.CircuitBreaker.Timeout, 30 * time.Second},
		{"CircuitBreaker.MaxRequests", cfg.CircuitBreaker.MaxRequests, 3},
		// CORS
		{"CORS.MaxAge", cfg.CORS.MaxAge, 3600},
		// Observability
		{"Observability.LogLevel", cfg.Observability.LogLevel, "info"},
		{"Observability.TracingEnabled", cfg.Observability.TracingEnabled, false},
		{"Observability.TracingEndpoint", cfg.Observability.TracingEndpoint, ""},
		{"Observability.TracingInsecure", cfg.Observability.TracingInsecure, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
			}
		})
	}

	// Slice defaults checked separately.
	if len(cfg.Upload.AllowedTypes) != 1 || cfg.Upload.AllowedTypes[0] != "application/pdf" {
		t.Errorf("Upload.AllowedTypes = %v, want [application/pdf]", cfg.Upload.AllowedTypes)
	}
	if cfg.CORS.AllowedOrigins != nil {
		t.Errorf("CORS.AllowedOrigins = %v, want nil", cfg.CORS.AllowedOrigins)
	}
}

// --- Topic defaults ---

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
		{"TopicProcessDocument", cfg.Broker.TopicProcessDocument, "dp.commands.process-document"},
		{"TopicCompareVersions", cfg.Broker.TopicCompareVersions, "dp.commands.compare-versions"},
		{"TopicDPStatusChanged", cfg.Broker.TopicDPStatusChanged, "dp.events.status-changed"},
		{"TopicDPProcessingCompleted", cfg.Broker.TopicDPProcessingCompleted, "dp.events.processing-completed"},
		{"TopicDPProcessingFailed", cfg.Broker.TopicDPProcessingFailed, "dp.events.processing-failed"},
		{"TopicDPComparisonCompleted", cfg.Broker.TopicDPComparisonCompleted, "dp.events.comparison-completed"},
		{"TopicDPComparisonFailed", cfg.Broker.TopicDPComparisonFailed, "dp.events.comparison-failed"},
		{"TopicLICStatusChanged", cfg.Broker.TopicLICStatusChanged, "lic.events.status-changed"},
		{"TopicREStatusChanged", cfg.Broker.TopicREStatusChanged, "re.events.status-changed"},
		{"TopicDMVersionArtifactsReady", cfg.Broker.TopicDMVersionArtifactsReady, "dm.events.version-artifacts-ready"},
		{"TopicDMVersionAnalysisReady", cfg.Broker.TopicDMVersionAnalysisReady, "dm.events.version-analysis-ready"},
		{"TopicDMVersionReportsReady", cfg.Broker.TopicDMVersionReportsReady, "dm.events.version-reports-ready"},
		{"TopicDMVersionPartiallyAvail", cfg.Broker.TopicDMVersionPartiallyAvail, "dm.events.version-partially-available"},
		{"TopicDMVersionCreated", cfg.Broker.TopicDMVersionCreated, "dm.events.version-created"},
	}

	for _, tc := range topics {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// --- Override values ---

func TestLoad_OverrideValues(t *testing.T) {
	setRequiredEnv(t)

	t.Setenv("ORCH_HTTP_PORT", "3000")
	t.Setenv("ORCH_METRICS_PORT", "9191")
	t.Setenv("ORCH_REQUEST_TIMEOUT", "10s")
	t.Setenv("ORCH_UPLOAD_TIMEOUT", "120s")
	t.Setenv("ORCH_SHUTDOWN_TIMEOUT", "15s")
	t.Setenv("ORCH_BROKER_TLS", "true")
	t.Setenv("ORCH_BROKER_PREFETCH", "20")
	t.Setenv("ORCH_STORAGE_REGION", "ru-central3")
	t.Setenv("ORCH_STORAGE_UPLOAD_TIMEOUT", "45s")
	t.Setenv("ORCH_REDIS_PASSWORD", "secret")
	t.Setenv("ORCH_REDIS_DB", "2")
	t.Setenv("ORCH_REDIS_POOL_SIZE", "30")
	t.Setenv("ORCH_REDIS_TIMEOUT", "5s")
	t.Setenv("ORCH_UPLOAD_MAX_SIZE", "10485760")
	t.Setenv("ORCH_UPLOAD_ALLOWED_TYPES", "application/pdf, application/msword")
	t.Setenv("ORCH_UPLOAD_MAX_CONCURRENT", "5")
	t.Setenv("ORCH_DM_TIMEOUT_READ", "3s")
	t.Setenv("ORCH_DM_TIMEOUT_WRITE", "7s")
	t.Setenv("ORCH_DM_RETRY_MAX", "5")
	t.Setenv("ORCH_DM_RETRY_BACKOFF", "500ms")
	t.Setenv("ORCH_OPM_BASE_URL", "http://opm:8080")
	t.Setenv("ORCH_OPM_TIMEOUT", "3s")
	t.Setenv("ORCH_UOM_BASE_URL", "http://uom:8080")
	t.Setenv("ORCH_UOM_TIMEOUT", "3s")
	t.Setenv("ORCH_SSE_HEARTBEAT_INTERVAL", "10s")
	t.Setenv("ORCH_SSE_MAX_CONNECTION_AGE", "12h")
	t.Setenv("ORCH_RATELIMIT_ENABLED", "false")
	t.Setenv("ORCH_RATELIMIT_READ_RPS", "100")
	t.Setenv("ORCH_RATELIMIT_WRITE_RPS", "25")
	t.Setenv("ORCH_CB_FAILURE_THRESHOLD", "10")
	t.Setenv("ORCH_CB_TIMEOUT", "60s")
	t.Setenv("ORCH_CB_MAX_REQUESTS", "5")
	t.Setenv("ORCH_CORS_ALLOWED_ORIGINS", "https://app.contractpro.ru, https://staging.contractpro.ru")
	t.Setenv("ORCH_CORS_MAX_AGE", "7200")
	t.Setenv("ORCH_LOG_LEVEL", "debug")
	t.Setenv("ORCH_TRACING_ENABLED", "true")
	t.Setenv("ORCH_TRACING_ENDPOINT", "http://jaeger:4318")
	t.Setenv("ORCH_TRACING_INSECURE", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// HTTP
	if cfg.HTTP.Port != 3000 {
		t.Errorf("HTTP.Port = %d, want 3000", cfg.HTTP.Port)
	}
	if cfg.HTTP.MetricsPort != 9191 {
		t.Errorf("HTTP.MetricsPort = %d, want 9191", cfg.HTTP.MetricsPort)
	}
	if cfg.HTTP.RequestTimeout != 10*time.Second {
		t.Errorf("HTTP.RequestTimeout = %v, want 10s", cfg.HTTP.RequestTimeout)
	}
	if cfg.HTTP.UploadTimeout != 120*time.Second {
		t.Errorf("HTTP.UploadTimeout = %v, want 120s", cfg.HTTP.UploadTimeout)
	}
	if cfg.HTTP.ShutdownTimeout != 15*time.Second {
		t.Errorf("HTTP.ShutdownTimeout = %v, want 15s", cfg.HTTP.ShutdownTimeout)
	}

	// Broker
	if !cfg.Broker.TLS {
		t.Error("Broker.TLS should be true")
	}
	if cfg.Broker.Prefetch != 20 {
		t.Errorf("Broker.Prefetch = %d, want 20", cfg.Broker.Prefetch)
	}

	// Storage
	if cfg.Storage.Region != "ru-central3" {
		t.Errorf("Storage.Region = %q, want %q", cfg.Storage.Region, "ru-central3")
	}
	if cfg.Storage.UploadTimeout != 45*time.Second {
		t.Errorf("Storage.UploadTimeout = %v, want 45s", cfg.Storage.UploadTimeout)
	}

	// Redis
	if cfg.Redis.Password != "secret" {
		t.Errorf("Redis.Password = %q, want %q", cfg.Redis.Password, "secret")
	}
	if cfg.Redis.DB != 2 {
		t.Errorf("Redis.DB = %d, want 2", cfg.Redis.DB)
	}
	if cfg.Redis.PoolSize != 30 {
		t.Errorf("Redis.PoolSize = %d, want 30", cfg.Redis.PoolSize)
	}
	if cfg.Redis.Timeout != 5*time.Second {
		t.Errorf("Redis.Timeout = %v, want 5s", cfg.Redis.Timeout)
	}

	// Upload
	if cfg.Upload.MaxSize != 10485760 {
		t.Errorf("Upload.MaxSize = %d, want 10485760", cfg.Upload.MaxSize)
	}
	if len(cfg.Upload.AllowedTypes) != 2 {
		t.Fatalf("Upload.AllowedTypes length = %d, want 2", len(cfg.Upload.AllowedTypes))
	}
	if cfg.Upload.AllowedTypes[0] != "application/pdf" || cfg.Upload.AllowedTypes[1] != "application/msword" {
		t.Errorf("Upload.AllowedTypes = %v, want [application/pdf application/msword]", cfg.Upload.AllowedTypes)
	}
	if cfg.Upload.MaxConcurrent != 5 {
		t.Errorf("Upload.MaxConcurrent = %d, want 5", cfg.Upload.MaxConcurrent)
	}

	// DM Client
	if cfg.DMClient.TimeoutRead != 3*time.Second {
		t.Errorf("DMClient.TimeoutRead = %v, want 3s", cfg.DMClient.TimeoutRead)
	}
	if cfg.DMClient.TimeoutWrite != 7*time.Second {
		t.Errorf("DMClient.TimeoutWrite = %v, want 7s", cfg.DMClient.TimeoutWrite)
	}
	if cfg.DMClient.RetryMax != 5 {
		t.Errorf("DMClient.RetryMax = %d, want 5", cfg.DMClient.RetryMax)
	}
	if cfg.DMClient.RetryBackoff != 500*time.Millisecond {
		t.Errorf("DMClient.RetryBackoff = %v, want 500ms", cfg.DMClient.RetryBackoff)
	}

	// OPM Client
	if cfg.OPMClient.BaseURL != "http://opm:8080" {
		t.Errorf("OPMClient.BaseURL = %q, want %q", cfg.OPMClient.BaseURL, "http://opm:8080")
	}
	if cfg.OPMClient.Timeout != 3*time.Second {
		t.Errorf("OPMClient.Timeout = %v, want 3s", cfg.OPMClient.Timeout)
	}

	// UOM Client
	if cfg.UOMClient.BaseURL != "http://uom:8080" {
		t.Errorf("UOMClient.BaseURL = %q, want %q", cfg.UOMClient.BaseURL, "http://uom:8080")
	}
	if cfg.UOMClient.Timeout != 3*time.Second {
		t.Errorf("UOMClient.Timeout = %v, want 3s", cfg.UOMClient.Timeout)
	}

	// SSE
	if cfg.SSE.HeartbeatInterval != 10*time.Second {
		t.Errorf("SSE.HeartbeatInterval = %v, want 10s", cfg.SSE.HeartbeatInterval)
	}
	if cfg.SSE.MaxConnectionAge != 12*time.Hour {
		t.Errorf("SSE.MaxConnectionAge = %v, want 12h", cfg.SSE.MaxConnectionAge)
	}

	// Rate Limit
	if cfg.RateLimit.Enabled {
		t.Error("RateLimit.Enabled should be false")
	}
	if cfg.RateLimit.ReadRPS != 100 {
		t.Errorf("RateLimit.ReadRPS = %d, want 100", cfg.RateLimit.ReadRPS)
	}
	if cfg.RateLimit.WriteRPS != 25 {
		t.Errorf("RateLimit.WriteRPS = %d, want 25", cfg.RateLimit.WriteRPS)
	}

	// Circuit Breaker
	if cfg.CircuitBreaker.FailureThreshold != 10 {
		t.Errorf("CircuitBreaker.FailureThreshold = %d, want 10", cfg.CircuitBreaker.FailureThreshold)
	}
	if cfg.CircuitBreaker.Timeout != 60*time.Second {
		t.Errorf("CircuitBreaker.Timeout = %v, want 60s", cfg.CircuitBreaker.Timeout)
	}
	if cfg.CircuitBreaker.MaxRequests != 5 {
		t.Errorf("CircuitBreaker.MaxRequests = %d, want 5", cfg.CircuitBreaker.MaxRequests)
	}

	// CORS
	if len(cfg.CORS.AllowedOrigins) != 2 {
		t.Fatalf("CORS.AllowedOrigins length = %d, want 2", len(cfg.CORS.AllowedOrigins))
	}
	if cfg.CORS.AllowedOrigins[0] != "https://app.contractpro.ru" {
		t.Errorf("CORS.AllowedOrigins[0] = %q, want %q", cfg.CORS.AllowedOrigins[0], "https://app.contractpro.ru")
	}
	if cfg.CORS.AllowedOrigins[1] != "https://staging.contractpro.ru" {
		t.Errorf("CORS.AllowedOrigins[1] = %q, want %q", cfg.CORS.AllowedOrigins[1], "https://staging.contractpro.ru")
	}
	if cfg.CORS.MaxAge != 7200 {
		t.Errorf("CORS.MaxAge = %d, want 7200", cfg.CORS.MaxAge)
	}

	// Observability
	if cfg.Observability.LogLevel != "debug" {
		t.Errorf("Observability.LogLevel = %q, want %q", cfg.Observability.LogLevel, "debug")
	}
	if !cfg.Observability.TracingEnabled {
		t.Error("Observability.TracingEnabled should be true")
	}
	if cfg.Observability.TracingEndpoint != "http://jaeger:4318" {
		t.Errorf("Observability.TracingEndpoint = %q, want %q", cfg.Observability.TracingEndpoint, "http://jaeger:4318")
	}
	if !cfg.Observability.TracingInsecure {
		t.Error("Observability.TracingInsecure should be true")
	}
}

func TestLoad_OverrideTopics(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_BROKER_TOPIC_PROCESS_DOCUMENT", "custom.commands.process")
	t.Setenv("ORCH_BROKER_TOPIC_DM_VERSION_CREATED", "custom.dm.version-created")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Broker.TopicProcessDocument != "custom.commands.process" {
		t.Errorf("TopicProcessDocument = %q, want %q", cfg.Broker.TopicProcessDocument, "custom.commands.process")
	}
	if cfg.Broker.TopicDMVersionCreated != "custom.dm.version-created" {
		t.Errorf("TopicDMVersionCreated = %q, want %q", cfg.Broker.TopicDMVersionCreated, "custom.dm.version-created")
	}
	// Non-overridden topic should keep default.
	if cfg.Broker.TopicCompareVersions != "dp.commands.compare-versions" {
		t.Errorf("TopicCompareVersions = %q, want default", cfg.Broker.TopicCompareVersions)
	}
}

// --- Validation: port collision ---

func TestValidate_PortCollision(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_HTTP_PORT", "9090") // same as default ORCH_METRICS_PORT

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for port collision, got nil")
	}
	if !strings.Contains(err.Error(), "ORCH_HTTP_PORT and ORCH_METRICS_PORT must differ") {
		t.Errorf("error should mention port collision, got: %s", err.Error())
	}
}

// --- Validation: broker prefetch ---

func TestValidate_BrokerPrefetchZero(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_BROKER_PREFETCH", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for prefetch < 1, got nil")
	}
	if !strings.Contains(err.Error(), "ORCH_BROKER_PREFETCH must be >= 1") {
		t.Errorf("error should mention prefetch, got: %s", err.Error())
	}
}

// --- Validation: rate limiting conditional ---

func TestValidate_RateLimitEnabledWithZeroRPS(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_RATELIMIT_ENABLED", "true")
	t.Setenv("ORCH_RATELIMIT_READ_RPS", "0")
	t.Setenv("ORCH_RATELIMIT_WRITE_RPS", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for zero RPS with rate limiting enabled, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "ORCH_RATELIMIT_READ_RPS") {
		t.Errorf("error should mention READ_RPS, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "ORCH_RATELIMIT_WRITE_RPS") {
		t.Errorf("error should mention WRITE_RPS, got: %s", errMsg)
	}
}

func TestValidate_RateLimitDisabledWithZeroRPS(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_RATELIMIT_ENABLED", "false")
	t.Setenv("ORCH_RATELIMIT_READ_RPS", "0")
	t.Setenv("ORCH_RATELIMIT_WRITE_RPS", "0")

	_, err := Load()
	if err != nil {
		t.Fatalf("should succeed when rate limiting is disabled, got: %v", err)
	}
}

// --- Validation: JWT file ---

func TestValidate_JWTFileNotExist(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_JWT_PUBLIC_KEY_PATH", "/nonexistent/path/jwt-public.pem")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for nonexistent JWT file, got nil")
	}
	if !strings.Contains(err.Error(), "ORCH_JWT_PUBLIC_KEY_PATH") {
		t.Errorf("error should mention ORCH_JWT_PUBLIC_KEY_PATH, got: %s", err.Error())
	}
}

// --- Validation: upload max size ---

func TestValidate_UploadMaxSizeZero(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_UPLOAD_MAX_SIZE", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for zero upload max size, got nil")
	}
	if !strings.Contains(err.Error(), "ORCH_UPLOAD_MAX_SIZE must be > 0") {
		t.Errorf("error should mention ORCH_UPLOAD_MAX_SIZE, got: %s", err.Error())
	}
}

// --- Validation: log level ---

func TestValidate_InvalidLogLevel(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_LOG_LEVEL", "verbose")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid log level, got nil")
	}
	if !strings.Contains(err.Error(), "ORCH_LOG_LEVEL must be one of") {
		t.Errorf("error should mention ORCH_LOG_LEVEL, got: %s", err.Error())
	}
}

// --- Validation: circuit breaker ---

func TestValidate_CBFailureThresholdZero(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_CB_FAILURE_THRESHOLD", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for zero CB failure threshold, got nil")
	}
	if !strings.Contains(err.Error(), "ORCH_CB_FAILURE_THRESHOLD must be >= 1") {
		t.Errorf("error should mention CB failure threshold, got: %s", err.Error())
	}
}

// --- Validation: redis pool size ---

func TestValidate_RedisPoolSizeZero(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ORCH_REDIS_POOL_SIZE", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for zero redis pool size, got nil")
	}
	if !strings.Contains(err.Error(), "ORCH_REDIS_POOL_SIZE must be >= 1") {
		t.Errorf("error should mention ORCH_REDIS_POOL_SIZE, got: %s", err.Error())
	}
}

// --- Invalid values fallback ---

func TestEnvHelpers_InvalidValues_FallbackToDefault(t *testing.T) {
	setRequiredEnv(t)

	t.Setenv("ORCH_HTTP_PORT", "not-a-number")
	t.Setenv("ORCH_UPLOAD_MAX_SIZE", "abc")
	t.Setenv("ORCH_REQUEST_TIMEOUT", "invalid-duration")
	t.Setenv("ORCH_BROKER_TLS", "maybe")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HTTP.Port != 8080 {
		t.Errorf("HTTP.Port should fall back to default, got %d", cfg.HTTP.Port)
	}
	if cfg.Upload.MaxSize != 20971520 {
		t.Errorf("Upload.MaxSize should fall back to default, got %d", cfg.Upload.MaxSize)
	}
	if cfg.HTTP.RequestTimeout != 30*time.Second {
		t.Errorf("HTTP.RequestTimeout should fall back to default, got %v", cfg.HTTP.RequestTimeout)
	}
	if cfg.Broker.TLS != false {
		t.Errorf("Broker.TLS should fall back to default, got %v", cfg.Broker.TLS)
	}
}

// --- envStringSlice ---

func TestEnvStringSlice_CommaSeparated(t *testing.T) {
	t.Setenv("TEST_SLICE", "a, b , c")
	got := envStringSlice("TEST_SLICE")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("envStringSlice = %v, want [a b c]", got)
	}
}

func TestEnvStringSlice_Empty(t *testing.T) {
	t.Setenv("TEST_SLICE_EMPTY", "")
	got := envStringSlice("TEST_SLICE_EMPTY")
	if got != nil {
		t.Errorf("envStringSlice with empty value = %v, want nil", got)
	}
}

func TestEnvStringSlice_Unset(t *testing.T) {
	got := envStringSlice("TEST_SLICE_NONEXISTENT_KEY_12345")
	if got != nil {
		t.Errorf("envStringSlice with unset key = %v, want nil", got)
	}
}

func TestEnvStringSlice_SingleValue(t *testing.T) {
	t.Setenv("TEST_SLICE_SINGLE", "only-one")
	got := envStringSlice("TEST_SLICE_SINGLE")
	if len(got) != 1 || got[0] != "only-one" {
		t.Errorf("envStringSlice = %v, want [only-one]", got)
	}
}

// --- envBool ---

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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_BOOL", tc.envValue)
			got := envBool("TEST_BOOL", tc.defaultVal)
			if got != tc.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", tc.envValue, tc.defaultVal, got, tc.want)
			}
		})
	}
}

func TestEnvBool_EmptyFallsBackToDefault(t *testing.T) {
	got := envBool("TEST_BOOL_UNSET_12345", true)
	if !got {
		t.Error("envBool with unset var should return default true")
	}
}

func TestEnvBool_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_BOOL_INVALID", "not-a-bool")
	got := envBool("TEST_BOOL_INVALID", true)
	if !got {
		t.Error("envBool with invalid value should return default true")
	}
}

// --- Validate directly ---

func TestValidate_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty config")
	}
	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Errorf("unexpected error format: %s", err.Error())
	}
}

func TestValidate_FullConfig(t *testing.T) {
	keyPath := createTempKeyFile(t)
	cfg := &Config{
		HTTP: HTTPConfig{
			Port:            8080,
			MetricsPort:     9090,
			RequestTimeout:  30 * time.Second,
			UploadTimeout:   60 * time.Second,
			ShutdownTimeout: 30 * time.Second,
		},
		Broker: BrokerConfig{
			Address:  "amqp://localhost:5672/",
			Prefetch: 10,
		},
		Storage: StorageConfig{
			Endpoint:      "http://localhost:9000",
			Bucket:        "uploads",
			AccessKey:     "ak",
			SecretKey:     "sk",
			UploadTimeout: 30 * time.Second,
		},
		Redis: RedisConfig{
			Address:  "localhost:6379",
			Timeout:  2 * time.Second,
			PoolSize: 20,
		},
		Upload: UploadConfig{MaxSize: 20971520},
		DMClient: DMClientConfig{
			BaseURL:      "http://dm:8080",
			TimeoutRead:  5 * time.Second,
			TimeoutWrite: 10 * time.Second,
			RetryBackoff: 200 * time.Millisecond,
		},
		JWT: JWTConfig{PublicKeyPath: keyPath},
		SSE: SSEConfig{
			HeartbeatInterval: 15 * time.Second,
			MaxConnectionAge:  24 * time.Hour,
		},
		RateLimit: RateLimitConfig{Enabled: false},
		CircuitBreaker: CircuitBreakerConfig{
			FailureThreshold: 5,
			Timeout:          30 * time.Second,
			MaxRequests:      3,
		},
		Observability:    ObservabilityConfig{LogLevel: "info"},
		TypeConfirmation: TypeConfirmationConfig{ConfirmationTimeout: 24 * time.Hour, IdempotencyTTL: 60 * time.Second, WatchdogScanInterval: 1 * time.Minute},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}
