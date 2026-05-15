package openai

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OpenAIConfig is the adapter-local configuration. The wiring layer maps
// internal/config.OpenAIProviderConfig onto this struct so the adapter stays
// hermetic and reusable from tests / mocks without importing the global
// configuration package (mirrors claude.ClaudeConfig — code-architect Q1).
//
// There is intentionally NO PromptCacheEnabled field: OpenAI prompt caching is
// implicit (automatic prefix caching, not request-controlled) and v1 does not
// track it (CachedInputTokens is always 0 — llm-provider-abstraction.md §5.1,
// acceptance criteria LIC-TASK-015).
type OpenAIConfig struct {
	// APIKey is the OpenAI API key (LIC_OPENAI_API_KEY). Required.
	// Never logged; emitted only in the Authorization: Bearer request header.
	APIKey string

	// BaseURL is the API host root, e.g. "https://api.openai.com"
	// (LIC_OPENAI_API_BASE_URL). Required. Must include a scheme; http is
	// accepted for local mocks (httptest servers), https is enforced for
	// production by the config layer (configuration.md §3).
	BaseURL string

	// Model is the OpenAI model identifier sent in each request, e.g.
	// "gpt-4.1" (LIC_OPENAI_MODEL). Required.
	Model string

	// HTTPClient is optional; when nil the adapter constructs a default client
	// with TLS 1.2+ and pooled transport (code-architect Q4). No client-level
	// Timeout — Complete / HealthCheck run under the caller's
	// context.WithTimeout (error-handling.md §7.3 hierarchical timeout
	// invariant). Tests inject httptest.Server's Client here.
	HTTPClient HTTPClient
}

// Validate reports configuration errors aggregated via errors.Join. Each
// surfaced misconfiguration is qualified with the env-var name an operator can
// fix, matching the config-package convention (mirrors claude.ClaudeConfig.Validate).
func (c OpenAIConfig) Validate() error {
	var errs []error
	if strings.TrimSpace(c.APIKey) == "" {
		errs = append(errs, errors.New("openai: APIKey must not be empty (LIC_OPENAI_API_KEY)"))
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		errs = append(errs, errors.New("openai: BaseURL must not be empty (LIC_OPENAI_API_BASE_URL)"))
	} else {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("openai: BaseURL is not a valid URL: %q", c.BaseURL))
		} else {
			if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
				errs = append(errs, fmt.Errorf("openai: BaseURL must use http or https scheme, got %q", u.Scheme))
			}
			// Reject userinfo (user:pass@host) — credentials must travel in
			// the Authorization header, never embedded in a URL. Defense in
			// depth against the *url.Error path that would otherwise echo the
			// URL into a logged error string (mirrors claude config S1).
			if u.User != nil {
				errs = append(errs, errors.New("openai: BaseURL must not contain userinfo (auth must use Authorization header, not embedded URL credentials)"))
			}
		}
	}
	if strings.TrimSpace(c.Model) == "" {
		errs = append(errs, errors.New("openai: Model must not be empty (LIC_OPENAI_MODEL)"))
	}
	return errors.Join(errs...)
}

// HTTPClient is declared in provider.go alongside the type that consumes it.

// defaultHTTPClient builds a hardened *http.Client used when OpenAIConfig
// leaves HTTPClient nil.
//
//   - TLS 1.2+ is enforced at the transport layer (security.md §3 production
//     invariant; configuration.md §3 enforces https:// at the config layer,
//     this is defense-in-depth on top).
//   - No client-level Timeout — the caller's context.WithTimeout owns the
//     per-call budget (error-handling.md §7.3). A double-budget would cause
//     surprise cancellations before the agent timeout fires.
//   - Pooled connections matching the Claude sibling (familiar tuning):
//     MaxIdleConns=10, MaxIdleConnsPerHost=5, IdleConnTimeout=90s,
//     TLSHandshakeTimeout=10s.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}
