package claude

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClaudeConfig is the adapter-local configuration. The wiring layer maps
// internal/config.ClaudeProviderConfig onto this struct so the adapter stays
// hermetic and reusable from tests / mocks without importing the global
// configuration package (code-architect Q1).
type ClaudeConfig struct {
	// APIKey is the Anthropic API key (LIC_CLAUDE_API_KEY). Required.
	// Never logged; emitted only in the x-api-key request header.
	APIKey string

	// BaseURL is the Messages API host root, e.g. "https://api.anthropic.com"
	// (LIC_CLAUDE_API_BASE_URL). Required. Must include a scheme; http is
	// accepted for local mocks (httptest servers), https is enforced for
	// production by the config layer (configuration.md §3 rule 10).
	BaseURL string

	// Model is the Anthropic model identifier sent in each request, e.g.
	// "claude-sonnet-4-6" (LIC_CLAUDE_MODEL). Required.
	Model string

	// PromptCacheEnabled toggles cache_control: ephemeral on the system block
	// (LIC_CLAUDE_PROMPT_CACHE_ENABLED). When true, Anthropic Prompt Caching
	// reduces input cost on cache hit by ~10× (llm-provider-abstraction.md §5.1).
	PromptCacheEnabled bool

	// HTTPClient is optional; when nil the adapter constructs a default client
	// with TLS 1.2+ and pooled transport (code-architect Q4). No client-level
	// Timeout — Complete / HealthCheck run under the caller's
	// context.WithTimeout (error-handling.md §7.3 hierarchical timeout
	// invariant). Tests inject httptest.Server's Client here.
	HTTPClient HTTPClient
}

// Validate reports configuration errors aggregated via errors.Join. Each
// surfaced misconfiguration is qualified with the env-var name an operator can
// fix, matching the config-package convention.
func (c ClaudeConfig) Validate() error {
	var errs []error
	if strings.TrimSpace(c.APIKey) == "" {
		errs = append(errs, errors.New("claude: APIKey must not be empty (LIC_CLAUDE_API_KEY)"))
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		errs = append(errs, errors.New("claude: BaseURL must not be empty (LIC_CLAUDE_API_BASE_URL)"))
	} else {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("claude: BaseURL is not a valid URL: %q", c.BaseURL))
		} else {
			if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
				errs = append(errs, fmt.Errorf("claude: BaseURL must use http or https scheme, got %q", u.Scheme))
			}
			// Reject userinfo (user:pass@host) — credentials must travel in
			// the x-api-key header, never embedded in a URL. Defense in depth
			// against the *url.Error path that would otherwise echo the URL
			// into a logged error string. security-engineer review S1.
			if u.User != nil {
				errs = append(errs, errors.New("claude: BaseURL must not contain userinfo (auth must use x-api-key header, not embedded URL credentials)"))
			}
		}
	}
	if strings.TrimSpace(c.Model) == "" {
		errs = append(errs, errors.New("claude: Model must not be empty (LIC_CLAUDE_MODEL)"))
	}
	return errors.Join(errs...)
}

// HTTPClient is declared in provider.go alongside the type that consumes it.

// defaultHTTPClient builds a hardened *http.Client used when ClaudeConfig
// leaves HTTPClient nil.
//
//   - TLS 1.2+ is enforced at the transport layer (security.md §3 production
//     invariant; configuration.md §3 rule 10 enforces https:// at config
//     layer, this is defense-in-depth on top).
//   - No client-level Timeout — the caller's context.WithTimeout owns the
//     per-call budget (error-handling.md §7.3). A double-budget would cause
//     surprise cancellations 5 seconds before the agent timeout fires.
//   - Pooled connections matching the DP OCR adapter (familiar tuning):
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
