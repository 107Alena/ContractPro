package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Provider IDs (kept as local string constants until LIC-TASK-011 introduces
// `domain/model` types; consumers map onto domain types at boundary).
const (
	ProviderClaude = "claude"
	ProviderOpenAI = "openai"
	ProviderGemini = "gemini"
)

// LLMConfig holds shared LLM settings plus per-provider sub-configs.
type LLMConfig struct {
	ProviderFallbackOrder []string      // LIC_PROVIDER_FALLBACK_ORDER — ordered fallback chain
	RequestTimeout        time.Duration // LIC_LLM_REQUEST_TIMEOUT — per-call HTTP timeout
	ConcurrencyPerProvider int          // LIC_LLM_CONCURRENCY_PER_PROVIDER — max parallel calls per provider

	Claude ClaudeProviderConfig
	OpenAI OpenAIProviderConfig
	Gemini GeminiProviderConfig
}

// ClaudeProviderConfig holds Anthropic-specific settings.
type ClaudeProviderConfig struct {
	APIKey              string  // LIC_CLAUDE_API_KEY (conditionally required)
	BaseURL             string  // LIC_CLAUDE_API_BASE_URL
	Model               string  // LIC_CLAUDE_MODEL
	RPS                 float64 // LIC_CLAUDE_RPS
	Burst               int     // LIC_CLAUDE_BURST
	PromptCacheEnabled  bool    // LIC_CLAUDE_PROMPT_CACHE_ENABLED
}

// OpenAIProviderConfig holds OpenAI-specific settings.
type OpenAIProviderConfig struct {
	APIKey  string  // LIC_OPENAI_API_KEY (conditionally required)
	BaseURL string  // LIC_OPENAI_API_BASE_URL
	Model   string  // LIC_OPENAI_MODEL
	RPS     float64 // LIC_OPENAI_RPS
	Burst   int     // LIC_OPENAI_BURST
}

// GeminiProviderConfig holds Google Gemini-specific settings.
type GeminiProviderConfig struct {
	APIKey  string  // LIC_GEMINI_API_KEY (conditionally required)
	BaseURL string  // LIC_GEMINI_API_BASE_URL
	Model   string  // LIC_GEMINI_MODEL
	RPS     float64 // LIC_GEMINI_RPS
	Burst   int     // LIC_GEMINI_BURST
}

func loadLLMConfig() LLMConfig {
	return LLMConfig{
		ProviderFallbackOrder:  envList("LIC_PROVIDER_FALLBACK_ORDER", []string{ProviderClaude, ProviderOpenAI, ProviderGemini}),
		RequestTimeout:         envDuration("LIC_LLM_REQUEST_TIMEOUT", 60*time.Second),
		ConcurrencyPerProvider: envInt("LIC_LLM_CONCURRENCY_PER_PROVIDER", 10),

		Claude: ClaudeProviderConfig{
			APIKey:             envString("LIC_CLAUDE_API_KEY", ""),
			BaseURL:            envString("LIC_CLAUDE_API_BASE_URL", "https://api.anthropic.com"),
			Model:              envString("LIC_CLAUDE_MODEL", "claude-sonnet-4-6"),
			RPS:                envFloat64("LIC_CLAUDE_RPS", 10),
			Burst:              envInt("LIC_CLAUDE_BURST", 20),
			PromptCacheEnabled: envBool("LIC_CLAUDE_PROMPT_CACHE_ENABLED", true),
		},
		OpenAI: OpenAIProviderConfig{
			APIKey:  envString("LIC_OPENAI_API_KEY", ""),
			BaseURL: envString("LIC_OPENAI_API_BASE_URL", "https://api.openai.com"),
			Model:   envString("LIC_OPENAI_MODEL", "gpt-4.1"),
			RPS:     envFloat64("LIC_OPENAI_RPS", 20),
			Burst:   envInt("LIC_OPENAI_BURST", 40),
		},
		Gemini: GeminiProviderConfig{
			APIKey:  envString("LIC_GEMINI_API_KEY", ""),
			BaseURL: envString("LIC_GEMINI_API_BASE_URL", "https://generativelanguage.googleapis.com"),
			Model:   envString("LIC_GEMINI_MODEL", "gemini-2.5-pro"),
			RPS:     envFloat64("LIC_GEMINI_RPS", 20),
			Burst:   envInt("LIC_GEMINI_BURST", 40),
		},
	}
}

// IsKnownProvider reports whether `id` is one of the three supported provider IDs.
func IsKnownProvider(id string) bool {
	switch id {
	case ProviderClaude, ProviderOpenAI, ProviderGemini:
		return true
	}
	return false
}

func (l LLMConfig) validate() error {
	var errs []error
	if len(l.ProviderFallbackOrder) == 0 {
		errs = append(errs, fmt.Errorf("config: LIC_PROVIDER_FALLBACK_ORDER must contain at least one provider"))
	}
	seen := make(map[string]struct{}, len(l.ProviderFallbackOrder))
	for _, p := range l.ProviderFallbackOrder {
		if !IsKnownProvider(p) {
			errs = append(errs, fmt.Errorf("config: LIC_PROVIDER_FALLBACK_ORDER contains unknown provider %q (expected claude|openai|gemini)", p))
			continue
		}
		if _, dup := seen[p]; dup {
			errs = append(errs, fmt.Errorf("config: LIC_PROVIDER_FALLBACK_ORDER contains duplicate provider %q", p))
		}
		seen[p] = struct{}{}
	}
	if l.RequestTimeout <= 0 {
		errs = append(errs, fmt.Errorf("config: LIC_LLM_REQUEST_TIMEOUT must be > 0, got %s", l.RequestTimeout))
	}
	if l.ConcurrencyPerProvider < 1 {
		errs = append(errs, fmt.Errorf("config: LIC_LLM_CONCURRENCY_PER_PROVIDER must be >= 1, got %d", l.ConcurrencyPerProvider))
	}

	// Per-provider sub-config validation. Validate even providers not in the
	// fallback chain so a misconfigured operator-provided field still surfaces.
	if err := l.Claude.validate(); err != nil {
		errs = append(errs, err)
	}
	if err := l.OpenAI.validate(); err != nil {
		errs = append(errs, err)
	}
	if err := l.Gemini.validate(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// validateProviderKeys checks that every provider listed in the fallback chain
// has a non-empty API key. Providers absent from the chain may have empty keys.
func (l LLMConfig) validateProviderKeys() error {
	for _, p := range l.ProviderFallbackOrder {
		switch p {
		case ProviderClaude:
			if l.Claude.APIKey == "" {
				return missingVarErr("LIC_CLAUDE_API_KEY (required because claude is in LIC_PROVIDER_FALLBACK_ORDER)")
			}
		case ProviderOpenAI:
			if l.OpenAI.APIKey == "" {
				return missingVarErr("LIC_OPENAI_API_KEY (required because openai is in LIC_PROVIDER_FALLBACK_ORDER)")
			}
		case ProviderGemini:
			if l.Gemini.APIKey == "" {
				return missingVarErr("LIC_GEMINI_API_KEY (required because gemini is in LIC_PROVIDER_FALLBACK_ORDER)")
			}
		}
	}
	return nil
}

func (c ClaudeProviderConfig) validate() error {
	return validateProviderShape("CLAUDE", c.BaseURL, c.Model, c.RPS, c.Burst)
}

func (o OpenAIProviderConfig) validate() error {
	return validateProviderShape("OPENAI", o.BaseURL, o.Model, o.RPS, o.Burst)
}

func (g GeminiProviderConfig) validate() error {
	return validateProviderShape("GEMINI", g.BaseURL, g.Model, g.RPS, g.Burst)
}

func validateProviderShape(provider, baseURL, model string, rps float64, burst int) error {
	if baseURL == "" {
		return fmt.Errorf("config: LIC_%s_API_BASE_URL must not be empty", provider)
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("config: LIC_%s_API_BASE_URL is not a valid URL: %q", provider, baseURL)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return fmt.Errorf("config: LIC_%s_API_BASE_URL must use http:// or https://, got %q", provider, u.Scheme)
	}
	if model == "" {
		return fmt.Errorf("config: LIC_%s_MODEL must not be empty", provider)
	}
	if rps <= 0 {
		return fmt.Errorf("config: LIC_%s_RPS must be > 0, got %v", provider, rps)
	}
	if burst < 1 {
		return fmt.Errorf("config: LIC_%s_BURST must be >= 1, got %d", provider, burst)
	}
	return nil
}
