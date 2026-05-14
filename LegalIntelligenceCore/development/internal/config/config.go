// Package config loads, validates, and exposes LIC service configuration.
//
// All env vars use the LIC_ prefix. Required vars cause Load() to fail fast.
// Validation aggregates all errors via errors.Join so misconfiguration is
// surfaced in one shot at deployment, not one error at a time.
//
// Consumers (broker, redis, llm, agents, ...) receive their slice of *Config
// by composition; no package outside `config` should call os.Getenv directly.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config is the root configuration for the Legal Intelligence Core service.
type Config struct {
	App           AppConfig
	Broker        BrokerConfig
	Redis         RedisConfig
	Idempotency   IdempotencyConfig
	Pipeline      PipelineConfig
	LLM           LLMConfig
	Agents        AgentsConfig
	Scoring       ScoringConfig
	Observability ObservabilityConfig
	Pricing       PricingConfig
	Cache         CacheConfig
	Security      SecurityConfig
}

// Load reads configuration from environment variables, applies defaults,
// validates the result, and returns the populated Config.
//
// A .env file in the working directory is loaded first; already-set env
// vars take precedence over .env values. Missing or invalid required vars
// cause Load() to return an aggregated error.
func Load() (*Config, error) {
	// .env is optional for production (env injection from secret stores);
	// a missing or unreadable file is not an error.
	_ = godotenv.Load()

	cfg := &Config{
		App:           loadAppConfig(),
		Broker:        loadBrokerConfig(),
		Redis:         loadRedisConfig(),
		Idempotency:   loadIdempotencyConfig(),
		Pipeline:      loadPipelineConfig(),
		LLM:           loadLLMConfig(),
		Agents:        loadAgentsConfig(),
		Scoring:       loadScoringConfig(),
		Observability: loadObservabilityConfig(),
		Pricing:       loadPricingConfig(),
		Cache:         loadCacheConfig(),
		Security:      loadSecurityConfig(),
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate runs all sub-config validations plus the cross-cutting rules
// (TLS-in-production, conditional provider keys). All findings are joined
// into a single error so callers see the full misconfiguration surface.
func (c *Config) Validate() error {
	var errs []error

	errs = appendErr(errs, c.App.validate())
	errs = appendErr(errs, c.Broker.validate())
	errs = appendErr(errs, c.Redis.validate())
	errs = appendErr(errs, c.Idempotency.validate())
	errs = appendErr(errs, c.Pipeline.validate())
	errs = appendErr(errs, c.LLM.validate())
	errs = appendErr(errs, c.Agents.validate(c.LLM.ProviderFallbackOrder))
	errs = appendErr(errs, c.Scoring.validate())
	errs = appendErr(errs, c.Observability.validate())
	errs = appendErr(errs, c.Cache.validate())
	errs = appendErr(errs, c.Security.validate())

	// Conditional: providers in the fallback chain must have API keys.
	errs = appendErr(errs, c.LLM.validateProviderKeys())

	// Cross-cutting: staging/production must use TLS everywhere.
	errs = appendErr(errs, enforceTLS(c))

	return errors.Join(errs...)
}

func appendErr(errs []error, err error) []error {
	if err != nil {
		errs = append(errs, err)
	}
	return errs
}

// --- env helpers (string / int / int64 / duration / bool / list) ---

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

func envFloat64(key string, defaultVal float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseFloat(v, 64)
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

// envList parses a comma-separated list; whitespace around items is trimmed,
// empty items are dropped. Returns defaultVal when the env var is unset/empty.
func envList(key string, defaultVal []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return defaultVal
	}
	return out
}

// missingVarErr builds a deterministic, easily-greppable "missing var" error.
func missingVarErr(name string) error {
	return fmt.Errorf("config: required env var %s is missing", name)
}
