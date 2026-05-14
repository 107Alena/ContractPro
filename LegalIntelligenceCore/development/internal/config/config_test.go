package config

import (
	"strings"
	"testing"
	"time"
)

// fullEnv returns a complete LIC env map sufficient to make Load() succeed
// with defaults filling in everything not explicitly required.
func fullEnv() map[string]string {
	return map[string]string{
		"LIC_ENV":      "local",
		"LIC_BROKER_URL": "amqp://user:pass@localhost:5672/contractpro",
		"LIC_REDIS_URL":  "redis://localhost:6379",

		"LIC_CLAUDE_API_KEY": "sk-ant-test-key",
		"LIC_OPENAI_API_KEY": "sk-test-openai",
		"LIC_GEMINI_API_KEY": "AIza-test-gemini",

		"LIC_PROMPT_INJECTION_HASH_KEY": "test-pii-hmac-secret",
		"LIC_DLQ_HASH_KEY":              "test-dlq-hmac-secret",
	}
}

// setEnv populates the given env vars for the duration of t, clearing any
// LIC_* var first so a previous test cannot leak state between sub-tests.
//
// Note: setEnv relies on t.Setenv, which intentionally panics if called from
// a t.Parallel test. Do not mark tests in this file as parallel.
func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	// Clean every LIC_* var that we know touches Load() to avoid leakage.
	known := []string{
		"LIC_LOG_LEVEL", "LIC_ENV", "LIC_HTTP_PORT", "LIC_SHUTDOWN_TIMEOUT",
		"LIC_BROKER_URL", "LIC_BROKER_TLS", "LIC_CONSUMER_PREFETCH", "LIC_CONSUMER_MAX_REDELIVERIES",
		"LIC_CONSUMER_RETRY_TTL_1", "LIC_CONSUMER_RETRY_TTL_2", "LIC_CONSUMER_RETRY_TTL_3",
		"LIC_PUBLISHER_CONFIRM_TIMEOUT", "LIC_PUBLISH_BUFFER_SIZE",
		"LIC_REDIS_URL", "LIC_REDIS_DB", "LIC_REDIS_PASSWORD", "LIC_REDIS_TLS",
		"LIC_REDIS_POOL_SIZE", "LIC_REDIS_DIAL_TIMEOUT",
		"LIC_IDEMPOTENCY_TTL", "LIC_IDEMPOTENCY_PROCESSING_TTL",
		"LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL", "LIC_IDEMPOTENCY_FALLBACK_ENABLED",
		"LIC_PIPELINE_CONCURRENCY", "LIC_JOB_TIMEOUT", "LIC_DM_REQUEST_TIMEOUT",
		"LIC_DM_PERSIST_CONFIRM_TIMEOUT", "LIC_PENDING_CONFIRMATION_TTL",
		"LIC_PROVIDER_FALLBACK_ORDER", "LIC_LLM_REQUEST_TIMEOUT", "LIC_LLM_CONCURRENCY_PER_PROVIDER",
		"LIC_CLAUDE_API_KEY", "LIC_CLAUDE_API_BASE_URL", "LIC_CLAUDE_MODEL",
		"LIC_CLAUDE_RPS", "LIC_CLAUDE_BURST", "LIC_CLAUDE_PROMPT_CACHE_ENABLED",
		"LIC_OPENAI_API_KEY", "LIC_OPENAI_API_BASE_URL", "LIC_OPENAI_MODEL",
		"LIC_OPENAI_RPS", "LIC_OPENAI_BURST",
		"LIC_GEMINI_API_KEY", "LIC_GEMINI_API_BASE_URL", "LIC_GEMINI_MODEL",
		"LIC_GEMINI_RPS", "LIC_GEMINI_BURST",
		"LIC_CONFIDENCE_THRESHOLD", "LIC_MAX_INPUT_TOKENS", "LIC_MAX_AGENT_INPUT_TOKENS",
		"LIC_MAX_INGESTED_BYTES",
		"LIC_SCORE_WEIGHT_HIGH", "LIC_SCORE_WEIGHT_MEDIUM", "LIC_SCORE_WEIGHT_LOW",
		"LIC_SCORE_WEIGHT_MISSING_MANDATORY", "LIC_SCORE_WEIGHT_AMBIGUOUS_MANDATORY",
		"LIC_SCORE_LABEL_LOW_THRESHOLD", "LIC_SCORE_LABEL_MEDIUM_THRESHOLD",
		"LIC_OTEL_EXPORTER_OTLP_ENDPOINT", "LIC_OTEL_EXPORTER_OTLP_INSECURE",
		"LIC_OTEL_TRACES_SAMPLER", "LIC_OTEL_TRACES_SAMPLER_ARG",
		"LIC_OTEL_SERVICE_NAME", "LIC_METRICS_PATH",
		"LIC_PRICING_TABLE_PATH",
		"LIC_LLM_CACHE_ENABLED", "LIC_VERSION_META_CACHE_TTL",
		"LIC_PROMPT_INJECTION_HASH_KEY", "LIC_DLQ_HASH_KEY",
	}
	// per-agent
	for _, suffix := range []string{
		"TYPE_CLASSIFIER", "KEY_PARAMS", "PARTY_CONSISTENCY",
		"MANDATORY_CONDITIONS", "RISK_DETECTION", "RECOMMENDATION",
		"SUMMARY", "DETAILED_REPORT", "RISK_DELTA",
	} {
		known = append(known, "LIC_AGENT_"+suffix+"_PROVIDER", "LIC_AGENT_"+suffix+"_TIMEOUT")
	}
	for _, k := range known {
		t.Setenv(k, "")
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

// --- Load: happy path ---

func TestLoad_HappyPath_LocalDev(t *testing.T) {
	setEnv(t, fullEnv())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): unexpected error: %v", err)
	}
	if cfg.App.Env != EnvLocal {
		t.Errorf("App.Env = %q, want %q", cfg.App.Env, EnvLocal)
	}
	if cfg.Broker.URL != "amqp://user:pass@localhost:5672/contractpro" {
		t.Errorf("Broker.URL not populated")
	}
	if cfg.Redis.URL != "redis://localhost:6379" {
		t.Errorf("Redis.URL not populated")
	}
	// defaults
	if cfg.App.LogLevel != "info" {
		t.Errorf("default LogLevel = %q, want info", cfg.App.LogLevel)
	}
	if cfg.App.HTTPPort != 8080 {
		t.Errorf("default HTTPPort = %d, want 8080", cfg.App.HTTPPort)
	}
	if cfg.Pipeline.Concurrency != 5 {
		t.Errorf("default Pipeline.Concurrency = %d, want 5", cfg.Pipeline.Concurrency)
	}
	if cfg.Pipeline.JobTimeout != 90*time.Second {
		t.Errorf("default JobTimeout = %s, want 90s", cfg.Pipeline.JobTimeout)
	}
	if cfg.Scoring.ConfidenceThreshold != 0.75 {
		t.Errorf("default ConfidenceThreshold = %v, want 0.75", cfg.Scoring.ConfidenceThreshold)
	}
	if cfg.Scoring.MaxIngestedBytes != 10*1024*1024 {
		t.Errorf("default MaxIngestedBytes = %d, want 10 MiB", cfg.Scoring.MaxIngestedBytes)
	}
	if !cfg.LLM.Claude.PromptCacheEnabled {
		t.Errorf("default Claude.PromptCacheEnabled = false, want true")
	}
	// fallback chain default
	want := []string{ProviderClaude, ProviderOpenAI, ProviderGemini}
	got := cfg.LLM.ProviderFallbackOrder
	if len(got) != len(want) {
		t.Fatalf("default ProviderFallbackOrder = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("default ProviderFallbackOrder[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// per-agent defaults
	for _, id := range AllAgentIDs {
		if cfg.Agents.Providers[id] != ProviderClaude {
			t.Errorf("default Agents.Providers[%s] = %q, want %q", id, cfg.Agents.Providers[id], ProviderClaude)
		}
		if cfg.Agents.Timeouts[id] != defaultAgentTimeouts[id] {
			t.Errorf("default Agents.Timeouts[%s] = %s, want %s", id, cfg.Agents.Timeouts[id], defaultAgentTimeouts[id])
		}
	}
	if cfg.Security.PromptInjectionHashKey != "test-pii-hmac-secret" {
		t.Errorf("Security.PromptInjectionHashKey not populated")
	}
}

// --- Load: missing required vars are all surfaced at once ---

func TestLoad_MissingRequired_AggregatesAllErrors(t *testing.T) {
	setEnv(t, map[string]string{
		// nothing set — every required var is missing
	})
	_, err := Load()
	if err == nil {
		t.Fatal("Load(): expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"LIC_BROKER_URL",
		"LIC_REDIS_URL",
		"LIC_CLAUDE_API_KEY",
		"LIC_PROMPT_INJECTION_HASH_KEY",
		"LIC_DLQ_HASH_KEY",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error to mention %q, got: %s", want, msg)
		}
	}
}

func TestLoad_ConditionalProviderKey_OnlyClaudeInChain(t *testing.T) {
	env := fullEnv()
	env["LIC_PROVIDER_FALLBACK_ORDER"] = "claude"
	env["LIC_OPENAI_API_KEY"] = ""
	env["LIC_GEMINI_API_KEY"] = ""
	setEnv(t, env)
	if _, err := Load(); err != nil {
		t.Fatalf("Load(): expected no error when only claude is in chain and only its key is set, got %v", err)
	}
}

func TestLoad_ConditionalProviderKey_MissingForProviderInChain(t *testing.T) {
	env := fullEnv()
	env["LIC_PROVIDER_FALLBACK_ORDER"] = "claude,openai"
	env["LIC_OPENAI_API_KEY"] = "" // required because openai is in chain
	setEnv(t, env)
	_, err := Load()
	if err == nil {
		t.Fatal("Load(): expected error for missing LIC_OPENAI_API_KEY, got nil")
	}
	if !strings.Contains(err.Error(), "LIC_OPENAI_API_KEY") {
		t.Errorf("expected error to mention LIC_OPENAI_API_KEY, got: %s", err)
	}
}

// --- Load: invalid values ---

func TestLoad_InvalidConfidenceThreshold(t *testing.T) {
	env := fullEnv()
	env["LIC_CONFIDENCE_THRESHOLD"] = "1.2"
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LIC_CONFIDENCE_THRESHOLD") {
		t.Fatalf("Load(): expected error about LIC_CONFIDENCE_THRESHOLD, got: %v", err)
	}
}

func TestLoad_InvalidPipelineConcurrencyZero(t *testing.T) {
	env := fullEnv()
	env["LIC_PIPELINE_CONCURRENCY"] = "0"
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LIC_PIPELINE_CONCURRENCY") {
		t.Fatalf("Load(): expected error about LIC_PIPELINE_CONCURRENCY, got: %v", err)
	}
}

func TestLoad_InvalidMaxIngestedBytesBelowFloor(t *testing.T) {
	env := fullEnv()
	env["LIC_MAX_INGESTED_BYTES"] = "512" // < 1 MiB floor
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LIC_MAX_INGESTED_BYTES") {
		t.Fatalf("Load(): expected error about LIC_MAX_INGESTED_BYTES below 1 MiB floor, got: %v", err)
	}
}

func TestLoad_InvalidBrokerScheme(t *testing.T) {
	env := fullEnv()
	env["LIC_BROKER_URL"] = "http://nope:5672/"
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LIC_BROKER_URL") {
		t.Fatalf("Load(): expected error about LIC_BROKER_URL scheme, got: %v", err)
	}
}

func TestLoad_InvalidRedisScheme(t *testing.T) {
	env := fullEnv()
	env["LIC_REDIS_URL"] = "memcache://localhost:11211"
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LIC_REDIS_URL") {
		t.Fatalf("Load(): expected error about LIC_REDIS_URL scheme, got: %v", err)
	}
}

func TestLoad_InvalidFallbackChain_UnknownProvider(t *testing.T) {
	env := fullEnv()
	env["LIC_PROVIDER_FALLBACK_ORDER"] = "claude,unknown"
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("Load(): expected error about unknown provider, got: %v", err)
	}
}

func TestLoad_InvalidFallbackChain_Duplicate(t *testing.T) {
	env := fullEnv()
	env["LIC_PROVIDER_FALLBACK_ORDER"] = "claude,claude"
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Load(): expected error about duplicate provider, got: %v", err)
	}
}

func TestLoad_InvalidAgentProvider_NotInFallbackChain(t *testing.T) {
	env := fullEnv()
	env["LIC_PROVIDER_FALLBACK_ORDER"] = "claude"
	env["LIC_AGENT_TYPE_CLASSIFIER_PROVIDER"] = "openai" // not in chain
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LIC_AGENT_TYPE_CLASSIFIER_PROVIDER") {
		t.Fatalf("Load(): expected error about per-agent provider not in chain, got: %v", err)
	}
}

func TestLoad_InvalidLabelThresholdOrder(t *testing.T) {
	env := fullEnv()
	env["LIC_SCORE_LABEL_LOW_THRESHOLD"] = "0.30"
	env["LIC_SCORE_LABEL_MEDIUM_THRESHOLD"] = "0.50" // must be < LOW
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LABEL_MEDIUM_THRESHOLD") {
		t.Fatalf("Load(): expected error about label threshold ordering, got: %v", err)
	}
}

func TestLoad_InvalidIdempotencyHeartbeatTooLarge(t *testing.T) {
	env := fullEnv()
	env["LIC_IDEMPOTENCY_PROCESSING_TTL"] = "30s"
	env["LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL"] = "60s"
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "HEARTBEAT_INTERVAL") {
		t.Fatalf("Load(): expected error about heartbeat exceeding processing TTL, got: %v", err)
	}
}

// --- TLS enforcement (staging / production) ---

func TestLoad_ProductionTLSEnforcement_RequireRedisTLS(t *testing.T) {
	env := fullEnv()
	env["LIC_ENV"] = "production"
	// production-correct broker/llm URLs, but redis without TLS
	env["LIC_BROKER_URL"] = "amqps://user:pass@host:5671/contractpro"
	env["LIC_REDIS_URL"] = "redis://host:6379"
	env["LIC_REDIS_TLS"] = "false"
	setEnv(t, env)
	_, err := Load()
	if err == nil {
		t.Fatal("Load(): expected TLS-enforcement error in production, got nil")
	}
	if !strings.Contains(err.Error(), "LIC_REDIS_TLS") {
		t.Errorf("expected error to mention LIC_REDIS_TLS, got: %s", err)
	}
}

func TestLoad_ProductionTLSEnforcement_RequireAmqps(t *testing.T) {
	env := fullEnv()
	env["LIC_ENV"] = "staging"
	env["LIC_BROKER_URL"] = "amqp://user:pass@host:5672/contractpro" // not amqps
	env["LIC_REDIS_URL"] = "rediss://host:6379"
	env["LIC_REDIS_TLS"] = "true"
	setEnv(t, env)
	_, err := Load()
	if err == nil {
		t.Fatal("Load(): expected TLS-enforcement error for amqp:// in staging, got nil")
	}
	if !strings.Contains(err.Error(), "LIC_BROKER_URL") {
		t.Errorf("expected error to mention LIC_BROKER_URL, got: %s", err)
	}
}

// TestLoad_ProductionTLSEnforcement_LiteralProductionAmqpFails matches the
// exact wording of LIC-TASK-002 test_step 4: LIC_ENV=production + amqp:// → FATAL.
func TestLoad_ProductionTLSEnforcement_LiteralProductionAmqpFails(t *testing.T) {
	env := fullEnv()
	env["LIC_ENV"] = "production"
	env["LIC_BROKER_URL"] = "amqp://user:pass@host:5672/contractpro"
	env["LIC_REDIS_URL"] = "rediss://host:6379"
	env["LIC_REDIS_TLS"] = "true"
	setEnv(t, env)
	_, err := Load()
	if err == nil {
		t.Fatal("Load(): expected FATAL TLS error for amqp:// in production, got nil")
	}
	if !strings.Contains(err.Error(), "LIC_BROKER_URL") {
		t.Errorf("expected error to mention LIC_BROKER_URL, got: %s", err)
	}
}

// TestLoad_ProductionTLSEnforcement_BrokerTLSFlagAcceptedAsAlternative
// closes the gap from configuration.md §3 rule 10 wording
// "LIC_BROKER_TLS=true OR amqps:// in URL". With LIC_BROKER_TLS=true and an
// amqp:// URL, production validation must pass.
func TestLoad_ProductionTLSEnforcement_BrokerTLSFlagAcceptedAsAlternative(t *testing.T) {
	env := fullEnv()
	env["LIC_ENV"] = "production"
	env["LIC_BROKER_URL"] = "amqp://user:pass@host:5672/contractpro"
	env["LIC_BROKER_TLS"] = "true"
	env["LIC_REDIS_URL"] = "rediss://host:6379"
	env["LIC_REDIS_TLS"] = "true"
	setEnv(t, env)
	if _, err := Load(); err != nil {
		t.Fatalf("Load(): expected no TLS error when LIC_BROKER_TLS=true compensates for amqp://, got %v", err)
	}
}

func TestLoad_ProductionTLSEnforcement_RequireHTTPSForLLM(t *testing.T) {
	env := fullEnv()
	env["LIC_ENV"] = "production"
	env["LIC_BROKER_URL"] = "amqps://user:pass@host:5671/contractpro"
	env["LIC_REDIS_URL"] = "rediss://host:6379"
	env["LIC_REDIS_TLS"] = "true"
	env["LIC_CLAUDE_API_BASE_URL"] = "http://api.anthropic.com" // not https
	setEnv(t, env)
	_, err := Load()
	if err == nil {
		t.Fatal("Load(): expected TLS-enforcement error for http:// Claude URL in production, got nil")
	}
	if !strings.Contains(err.Error(), "LIC_CLAUDE_API_BASE_URL") {
		t.Errorf("expected error to mention LIC_CLAUDE_API_BASE_URL, got: %s", err)
	}
}

func TestLoad_ProductionTLSEnforcement_AllowsLocalAmqp(t *testing.T) {
	env := fullEnv()
	env["LIC_ENV"] = "local" // local should permit amqp:// + non-TLS redis
	env["LIC_BROKER_URL"] = "amqp://user:pass@localhost:5672/contractpro"
	env["LIC_REDIS_URL"] = "redis://localhost:6379"
	env["LIC_REDIS_TLS"] = "false"
	setEnv(t, env)
	if _, err := Load(); err != nil {
		t.Fatalf("Load(): expected no TLS-enforcement error in local env, got %v", err)
	}
}

func TestLoad_ProductionTLSEnforcement_OTELInsecureForbidden(t *testing.T) {
	env := fullEnv()
	env["LIC_ENV"] = "production"
	env["LIC_BROKER_URL"] = "amqps://user:pass@host:5671/contractpro"
	env["LIC_REDIS_URL"] = "rediss://host:6379"
	env["LIC_REDIS_TLS"] = "true"
	env["LIC_OTEL_EXPORTER_OTLP_ENDPOINT"] = "otel-collector:4317"
	env["LIC_OTEL_EXPORTER_OTLP_INSECURE"] = "true"
	setEnv(t, env)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LIC_OTEL_EXPORTER_OTLP_INSECURE") {
		t.Fatalf("Load(): expected error about OTEL_INSECURE in production, got: %v", err)
	}
}

// --- env helpers ---

func TestEnvList_TrimsAndDropsEmpty(t *testing.T) {
	t.Setenv("LIC_PROVIDER_FALLBACK_ORDER", " claude , , openai ,gemini")
	got := envList("LIC_PROVIDER_FALLBACK_ORDER", nil)
	want := []string{"claude", "openai", "gemini"}
	if len(got) != len(want) {
		t.Fatalf("envList = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("envList[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEnvList_FallsBackOnEmpty(t *testing.T) {
	t.Setenv("LIC_PROVIDER_FALLBACK_ORDER", "")
	got := envList("LIC_PROVIDER_FALLBACK_ORDER", []string{"x"})
	if len(got) != 1 || got[0] != "x" {
		t.Errorf("envList fallback = %v, want [x]", got)
	}
}

func TestEnvDuration_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("LIC_JOB_TIMEOUT", "not-a-duration")
	if got := envDuration("LIC_JOB_TIMEOUT", 90*time.Second); got != 90*time.Second {
		t.Errorf("envDuration invalid value = %s, want default 90s", got)
	}
}

func TestEnvBool_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("LIC_REDIS_TLS", "neither-true-nor-false")
	if envBool("LIC_REDIS_TLS", true) != true {
		t.Errorf("envBool invalid = false, want default true")
	}
}

func TestEnvInt_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("LIC_PIPELINE_CONCURRENCY", "abc")
	if envInt("LIC_PIPELINE_CONCURRENCY", 7) != 7 {
		t.Errorf("envInt invalid = ..., want default 7")
	}
}

func TestEnvironment_IsProductionLike(t *testing.T) {
	cases := map[Environment]bool{
		EnvLocal:      false,
		EnvDev:        false,
		EnvStaging:    true,
		EnvProduction: true,
	}
	for env, want := range cases {
		if got := env.IsProductionLike(); got != want {
			t.Errorf("Environment(%q).IsProductionLike() = %v, want %v", env, got, want)
		}
	}
}

// --- Validate composes correctly on a constructed struct ---

func TestValidate_OnDirectStructInjection(t *testing.T) {
	cfg := &Config{
		App: AppConfig{
			LogLevel: "info", Env: EnvLocal, HTTPPort: 8080, ShutdownTimeout: time.Second,
		},
		Broker: BrokerConfig{
			URL: "amqp://h/", ConsumerPrefetch: 1, ConsumerMaxRedeliveries: 0,
			ConsumerRetryTTL1: time.Second, ConsumerRetryTTL2: time.Second, ConsumerRetryTTL3: time.Second,
			PublisherConfirmTimeout: time.Second, PublishBufferSize: 0,
		},
		Redis: RedisConfig{
			URL: "redis://h/", PoolSize: 1, DialTimeout: time.Second,
		},
		Idempotency: IdempotencyConfig{
			TTL: time.Hour, ProcessingTTL: 2 * time.Second, HeartbeatInterval: time.Second,
		},
		Pipeline: PipelineConfig{
			Concurrency: 1, JobTimeout: time.Second, DMRequestTimeout: time.Second,
			DMPersistConfirmTimeout: time.Second, PendingConfirmationTTL: time.Second,
		},
		LLM: LLMConfig{
			ProviderFallbackOrder:  []string{ProviderClaude},
			RequestTimeout:         time.Second,
			ConcurrencyPerProvider: 1,
			Claude: ClaudeProviderConfig{APIKey: "k", BaseURL: "https://x", Model: "m", RPS: 1, Burst: 1},
			OpenAI: OpenAIProviderConfig{BaseURL: "https://x", Model: "m", RPS: 1, Burst: 1},
			Gemini: GeminiProviderConfig{BaseURL: "https://x", Model: "m", RPS: 1, Burst: 1},
		},
		Agents: AgentsConfig{
			Providers: func() map[string]string {
				m := map[string]string{}
				for _, id := range AllAgentIDs {
					m[id] = ProviderClaude
				}
				return m
			}(),
			Timeouts: func() map[string]time.Duration {
				m := map[string]time.Duration{}
				for _, id := range AllAgentIDs {
					m[id] = time.Second
				}
				return m
			}(),
		},
		Scoring: ScoringConfig{
			WeightHigh: 25, WeightMedium: 10, WeightLow: 3,
			WeightMissingMandatory: 15, WeightAmbiguousMandatory: 5,
			LabelLowThreshold: 0.75, LabelMediumThreshold: 0.45,
			ConfidenceThreshold: 0.75, MaxInputTokens: 1000, MaxAgentInputTokens: 1000,
			MaxIngestedBytes: oneMiB,
		},
		Observability: ObservabilityConfig{
			TracesSamplerArg: 0.1, ServiceName: "lic-service", MetricsPath: "/metrics",
		},
		Cache: CacheConfig{
			VersionMetaCacheTTL: time.Hour,
		},
		Security: SecurityConfig{
			PromptInjectionHashKey: "k1", DLQHashKey: "k2",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(): unexpected error on valid struct: %v", err)
	}
}

// errors.Join behavior is relied on by Validate(); make sure it surfaces both
// errors when more than one validation fails.
func TestValidate_JoinsMultipleErrors(t *testing.T) {
	setEnv(t, map[string]string{
		"LIC_BROKER_URL": "amqp://h/",
		"LIC_REDIS_URL":  "redis://h/",
		"LIC_CLAUDE_API_KEY": "k",
		"LIC_OPENAI_API_KEY": "k",
		"LIC_GEMINI_API_KEY": "k",
		"LIC_PROMPT_INJECTION_HASH_KEY": "k1",
		"LIC_DLQ_HASH_KEY":              "k2",
		// two bad values:
		"LIC_PIPELINE_CONCURRENCY": "0",
		"LIC_CONFIDENCE_THRESHOLD": "2.0",
	})
	_, err := Load()
	if err == nil {
		t.Fatal("Load(): expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "LIC_PIPELINE_CONCURRENCY") || !strings.Contains(msg, "LIC_CONFIDENCE_THRESHOLD") {
		t.Errorf("expected joined error to mention both invalid vars, got: %s", msg)
	}
	// errors.Join formats children separated by '\n'; presence is our proof
	// that multiple errors were aggregated rather than only the first reported.
	if !strings.Contains(msg, "\n") {
		t.Errorf("expected joined error to span multiple lines, got: %s", msg)
	}
}
