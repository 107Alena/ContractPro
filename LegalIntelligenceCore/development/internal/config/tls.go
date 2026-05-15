package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// enforceTLS applies the staging/production TLS-everywhere rule
// (configuration.md §3 rule 10): Redis TLS=true, broker URL = amqps://,
// every configured LLM base URL = https://. Returns a single joined error
// listing every TLS violation found, so the operator sees them all at once.
func enforceTLS(c *Config) error {
	if !c.App.Env.IsProductionLike() {
		return nil
	}

	var errs []error

	if !c.Redis.UsesTLS() {
		errs = append(errs, fmt.Errorf("config: LIC_REDIS_TLS must be true (or LIC_REDIS_URL must use rediss://) when LIC_ENV=%s", c.App.Env))
	}
	if !c.Broker.usesTLS() {
		errs = append(errs, fmt.Errorf("config: LIC_BROKER_URL must use amqps:// when LIC_ENV=%s", c.App.Env))
	}

	llmEndpoints := map[string]string{
		"LIC_CLAUDE_API_BASE_URL": c.LLM.Claude.BaseURL,
		"LIC_OPENAI_API_BASE_URL": c.LLM.OpenAI.BaseURL,
		"LIC_GEMINI_API_BASE_URL": c.LLM.Gemini.BaseURL,
	}
	for varName, endpoint := range llmEndpoints {
		if endpoint == "" {
			continue
		}
		u, err := url.Parse(endpoint)
		if err != nil {
			errs = append(errs, fmt.Errorf("config: %s is not a valid URL: %w", varName, err))
			continue
		}
		if !strings.EqualFold(u.Scheme, "https") {
			errs = append(errs, fmt.Errorf("config: %s must use https:// when LIC_ENV=%s, got %q", varName, c.App.Env, endpoint))
		}
	}

	if c.Observability.OTELEndpoint != "" && c.Observability.OTELInsecure {
		errs = append(errs, fmt.Errorf("config: LIC_OTEL_EXPORTER_OTLP_INSECURE must be false when LIC_ENV=%s", c.App.Env))
	}

	return errors.Join(errs...)
}
