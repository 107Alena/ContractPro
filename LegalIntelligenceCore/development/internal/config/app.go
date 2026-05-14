package config

import (
	"fmt"
	"time"
)

// Environment is the deployment environment ladder: local → dev → staging → production.
// Staging and production are subject to TLS-everywhere enforcement.
type Environment string

const (
	EnvLocal      Environment = "local"
	EnvDev        Environment = "dev"
	EnvStaging    Environment = "staging"
	EnvProduction Environment = "production"
)

// IsProductionLike returns true for environments that must run with TLS on
// every external link (Redis, broker, LLM endpoints).
func (e Environment) IsProductionLike() bool {
	return e == EnvStaging || e == EnvProduction
}

// AppConfig holds top-level service settings.
type AppConfig struct {
	LogLevel        string        // LIC_LOG_LEVEL — debug|info|warn|error
	Env             Environment   // LIC_ENV — local|dev|staging|production
	HTTPPort        int           // LIC_HTTP_PORT
	ShutdownTimeout time.Duration // LIC_SHUTDOWN_TIMEOUT
}

func loadAppConfig() AppConfig {
	return AppConfig{
		LogLevel:        envString("LIC_LOG_LEVEL", "info"),
		Env:             Environment(envString("LIC_ENV", string(EnvLocal))),
		HTTPPort:        envInt("LIC_HTTP_PORT", 8080),
		ShutdownTimeout: envDuration("LIC_SHUTDOWN_TIMEOUT", 120*time.Second),
	}
}

func (a AppConfig) validate() error {
	switch a.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: LIC_LOG_LEVEL must be one of debug|info|warn|error, got %q", a.LogLevel)
	}
	switch a.Env {
	case EnvLocal, EnvDev, EnvStaging, EnvProduction:
	default:
		return fmt.Errorf("config: LIC_ENV must be one of local|dev|staging|production, got %q", a.Env)
	}
	if a.HTTPPort < 1 || a.HTTPPort > 65535 {
		return fmt.Errorf("config: LIC_HTTP_PORT must be in [1,65535], got %d", a.HTTPPort)
	}
	if a.ShutdownTimeout <= 0 {
		return fmt.Errorf("config: LIC_SHUTDOWN_TIMEOUT must be > 0, got %s", a.ShutdownTimeout)
	}
	return nil
}
