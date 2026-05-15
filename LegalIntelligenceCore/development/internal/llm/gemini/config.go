package gemini

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GeminiConfig is the adapter-local configuration. The wiring layer maps
// internal/config.GeminiProviderConfig onto this struct so the adapter stays
// hermetic and reusable from tests / mocks without importing the global
// configuration package (mirrors claude.ClaudeConfig / openai.OpenAIConfig —
// code-architect Q1).
//
// There is intentionally NO PromptCacheEnabled field: Gemini context caching
// is a separate `cachedContent` API endpoint (long-lived cache) which v1 does
// not use (CachedInputTokens is always 0 — llm-provider-abstraction.md §5.1,
// acceptance criteria LIC-TASK-016).
type GeminiConfig struct {
	// APIKey is the Google Gemini API key (LIC_GEMINI_API_KEY). Required.
	// Never logged; emitted only in the x-goog-api-key request header — never
	// the URL (see provider.go package doc, code-architect Q2).
	APIKey string

	// BaseURL is the API host root, e.g.
	// "https://generativelanguage.googleapis.com" (LIC_GEMINI_API_BASE_URL).
	// Required. Must include a scheme; http is accepted for local mocks
	// (httptest servers), https is enforced for production by the config
	// layer (configuration.md §3).
	BaseURL string

	// Model is the Gemini model identifier sent on the wire, e.g.
	// "gemini-2.5-pro" (LIC_GEMINI_MODEL). Required. Unlike Claude / OpenAI
	// the model id is interpolated into the request URL path
	// (/v1beta/models/{model}:generateContent), so it is validated against a
	// path-safe charset (^[A-Za-z0-9._-]+$) to keep it from breaking the
	// route or smuggling extra path segments (code-architect MUST-FIX #2).
	Model string

	// HTTPClient is optional; when nil the adapter constructs a default client
	// with TLS 1.2+ and pooled transport. No client-level Timeout —
	// Complete / HealthCheck run under the caller's context.WithTimeout
	// (error-handling.md §7.3 hierarchical timeout invariant). Tests inject
	// httptest.Server's Client here.
	HTTPClient HTTPClient
}

// Validate reports configuration errors aggregated via errors.Join. Each
// surfaced misconfiguration is qualified with the env-var name an operator can
// fix, matching the config-package convention (mirrors
// openai.OpenAIConfig.Validate).
func (c GeminiConfig) Validate() error {
	var errs []error
	if strings.TrimSpace(c.APIKey) == "" {
		errs = append(errs, errors.New("gemini: APIKey must not be empty (LIC_GEMINI_API_KEY)"))
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		errs = append(errs, errors.New("gemini: BaseURL must not be empty (LIC_GEMINI_API_BASE_URL)"))
	} else {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("gemini: BaseURL is not a valid URL: %q", c.BaseURL))
		} else {
			if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
				errs = append(errs, fmt.Errorf("gemini: BaseURL must use http or https scheme, got %q", u.Scheme))
			}
			// Reject userinfo (user:pass@host) — credentials must travel in
			// the x-goog-api-key header, never embedded in a URL. Defense in
			// depth against the *url.Error path that would otherwise echo the
			// URL into a logged error string (mirrors openai/claude config S1).
			if u.User != nil {
				errs = append(errs, errors.New("gemini: BaseURL must not contain userinfo (auth must use x-goog-api-key header, not embedded URL credentials)"))
			}
		}
	}
	if strings.TrimSpace(c.Model) == "" {
		errs = append(errs, errors.New("gemini: Model must not be empty (LIC_GEMINI_MODEL)"))
	} else if !isValidModelID(c.Model) {
		// The model id is interpolated into the URL path; a stray "/" or ":"
		// or whitespace would break routing or smuggle path segments
		// (code-architect MUST-FIX #2).
		errs = append(errs, fmt.Errorf("gemini: Model %q contains characters outside the path-safe set [A-Za-z0-9._-] (LIC_GEMINI_MODEL)", c.Model))
	}
	return errors.Join(errs...)
}

// isValidModelID reports whether s is a non-empty string drawn only from the
// path-safe charset Gemini model identifiers use ([A-Za-z0-9._-], e.g.
// "gemini-2.5-pro", "gemini-2.0-flash-001"). Implemented as a byte scan rather
// than regexp to keep the hot path (per-request override validation in
// Complete) allocation-free and the dependency surface minimal.
func isValidModelID(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z',
			ch >= 'A' && ch <= 'Z',
			ch >= '0' && ch <= '9',
			ch == '.', ch == '_', ch == '-':
		default:
			return false
		}
	}
	return true
}

// HTTPClient is declared in provider.go alongside the type that consumes it.

// defaultHTTPClient builds a hardened *http.Client used when GeminiConfig
// leaves HTTPClient nil.
//
//   - TLS 1.2+ is enforced at the transport layer (security.md §3 production
//     invariant; configuration.md §3 enforces https:// at the config layer,
//     this is defense-in-depth on top).
//   - No client-level Timeout — the caller's context.WithTimeout owns the
//     per-call budget (error-handling.md §7.3). A double-budget would cause
//     surprise cancellations before the agent timeout fires.
//   - Pooled connections matching the Claude / OpenAI siblings (familiar
//     tuning): MaxIdleConns=10, MaxIdleConnsPerHost=5, IdleConnTimeout=90s,
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
