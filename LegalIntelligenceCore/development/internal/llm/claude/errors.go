package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// apiErrorEnvelope is Anthropic's documented error JSON. We try-decode it on
// any non-2xx response; when decode fails we fall back to the status code
// alone — Router-side mapping is unchanged.
type apiErrorEnvelope struct {
	Type  string         `json:"type"`
	Error apiErrorDetail `json:"error"`
}

// apiErrorDetail is the inner Anthropic error block. The Type field
// distinguishes invalid_request_error vs authentication_error vs ... — we
// use it to refine the LLMErrorCode (e.g. context-length vs malformed) when
// the HTTP status alone is ambiguous.
type apiErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// mapTransportError translates errors returned by http.Client.Do (i.e. the
// call never made it to an HTTP status code) into a typed LLMProviderError.
//
// **Deviation from DP convention** (code-architect Q10): the DP OCR adapter
// passes context errors through raw. We do not — every error returned to
// the Router MUST unwrap to *LLMProviderError so the Router's typed-switch
// decisions stay total (llm-provider-abstraction.md §1.2 adapter invariant).
// Future maintainers must not "fix" this to match DP — the contract is
// different, and Router-side logic depends on the wrapper.
func mapTransportError(err error) *port.LLMProviderError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return port.NewLLMProviderError(port.LLMErrorTimeout, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return port.NewLLMProviderError(port.LLMErrorTimeout, err)
	}
	return port.NewLLMProviderError(port.LLMErrorNetwork, err)
}

// mapHTTPError converts a non-2xx HTTP response into a typed
// LLMProviderError. The body argument is the (possibly truncated) response
// body so we can read Anthropic's structured error envelope when present.
//
// The 400-class is split by error.type:
//   - content_policy_violation       → LLMErrorContentPolicy
//   - invalid_request_error  + "context" / "too long" hint → LLMErrorContextTooLong
//   - everything else                 → LLMErrorMalformedRequest
//
// The 429 path parses Retry-After per RFC 7231 (integer seconds OR HTTP-
// date) and attaches the duration when positive. parseRetryAfter is
// deterministic — a malformed header yields nil and the Router applies its
// default backoff.
func mapHTTPError(status int, headers http.Header, body []byte) *port.LLMProviderError {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return port.NewLLMProviderError(port.LLMErrorInvalidAPIKey, httpStatusError(status, body))

	case status == http.StatusTooManyRequests:
		err := port.NewLLMProviderError(port.LLMErrorRateLimit, httpStatusError(status, body))
		if d := parseRetryAfter(headers.Get("Retry-After")); d != nil {
			err.RetryAfter = d
		}
		return err

	case status == http.StatusRequestTimeout:
		return port.NewLLMProviderError(port.LLMErrorTimeout, httpStatusError(status, body))

	case status == 529: // Anthropic-specific "overloaded"
		return port.NewLLMProviderError(port.LLMErrorOverloaded, httpStatusError(status, body))

	case status >= 500:
		return port.NewLLMProviderError(port.LLMErrorServerError, httpStatusError(status, body))

	case status == http.StatusBadRequest:
		return classify400(headers, body, status)

	default:
		// Unknown status — default to SERVER_ERROR (retryable, fallback-
		// eligible) rather than MALFORMED. SERVER_ERROR lets the Router try
		// the same provider once and then move on, preserving fallback. A
		// MALFORMED default would lock out fallback for 451/421/etc. that
		// another provider could plausibly handle. golang-pro S9.
		return port.NewLLMProviderError(port.LLMErrorServerError, httpStatusError(status, body))
	}
}

// classify400 dispatches HTTP 400 by Anthropic's structured error.type.
// Falls back to LLMErrorMalformedRequest when the body cannot be decoded.
//
// Order matters — context-length and quota-exceeded checks run before the
// generic content-policy / malformed buckets so a message that mentions
// "quota" or "context length" wins over a substring match on
// "policy"/"content".
func classify400(headers http.Header, body []byte, status int) *port.LLMProviderError {
	env, ok := tryDecodeAPIError(body)
	if !ok {
		return port.NewLLMProviderError(port.LLMErrorMalformedRequest, httpStatusError(status, body))
	}
	if isContextLength(env.Error) {
		return port.NewLLMProviderError(port.LLMErrorContextTooLong, httpStatusError(status, body))
	}
	if isQuotaExceeded(env.Error) {
		// Anthropic occasionally returns quota issues at 400 instead of 429
		// (e.g. monthly cap hit). Surface as QuotaExceeded so the Router
		// marks the provider permanently unhealthy for ~24h and falls back
		// to a different provider (security-engineer review N4).
		return port.NewLLMProviderError(port.LLMErrorQuotaExceeded, httpStatusError(status, body))
	}
	if isContentPolicy(env.Error) {
		return port.NewLLMProviderError(port.LLMErrorContentPolicy, httpStatusError(status, body))
	}
	return port.NewLLMProviderError(port.LLMErrorMalformedRequest, httpStatusError(status, body))
}

// tryDecodeAPIError attempts to parse the Anthropic error envelope. Returns
// (envelope, false) when parsing fails OR the envelope is empty — the caller
// then uses status-code-only classification.
func tryDecodeAPIError(body []byte) (apiErrorEnvelope, bool) {
	var env apiErrorEnvelope
	if len(body) == 0 {
		return env, false
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return env, false
	}
	if env.Type == "" && env.Error.Type == "" && env.Error.Message == "" {
		return env, false
	}
	return env, true
}

// isContentPolicy returns true when Anthropic indicated the request was
// blocked for policy reasons. The exact error.type string has shifted across
// API versions — we accept any case-insensitive match of well-known tokens
// rather than a single literal so a wire-side rename does not break us.
func isContentPolicy(d apiErrorDetail) bool {
	t := strings.ToLower(d.Type)
	if strings.Contains(t, "content_policy") || strings.Contains(t, "policy_violation") {
		return true
	}
	m := strings.ToLower(d.Message)
	return strings.Contains(m, "content policy")
}

// isContextLength returns true when Anthropic's invalid_request_error
// indicates the prompt exceeded the model's context window. The token-
// estimator + truncation pass (LIC-TASK-021) is meant to prevent this; when
// it slips through we surface it explicitly so the pipeline FAILs with
// is_retryable=false rather than burning fallback budget.
func isContextLength(d apiErrorDetail) bool {
	m := strings.ToLower(d.Message)
	return strings.Contains(m, "context") && (strings.Contains(m, "too long") ||
		strings.Contains(m, "length") || strings.Contains(m, "exceeds"))
}

// isQuotaExceeded returns true when Anthropic indicates a billing or quota
// constraint was hit. Some plans surface this as a 400 with a structured
// "quota"-flavoured type/message rather than the 429 we'd expect; routing
// it explicitly into LLMErrorQuotaExceeded preserves the catalog semantics
// (security-engineer review N4).
func isQuotaExceeded(d apiErrorDetail) bool {
	t := strings.ToLower(d.Type)
	if strings.Contains(t, "quota") || strings.Contains(t, "billing") {
		return true
	}
	m := strings.ToLower(d.Message)
	return strings.Contains(m, "quota") || strings.Contains(m, "billing") ||
		strings.Contains(m, "credit balance")
}

// httpStatusError builds the wrapped underlying error attached to an
// LLMProviderError on the HTTP path. The body is truncated to keep error
// strings bounded; PII is never present in Anthropic's structured error
// payloads (they describe request shape, not document content).
//
// Truncation respects UTF-8 rune boundaries so a Cyrillic / emoji response
// from Anthropic doesn't produce a corrupt sequence in our logs
// (security-engineer review N1).
func httpStatusError(status int, body []byte) error {
	const maxBody = 512
	trimmed := body
	if len(trimmed) > maxBody {
		trimmed = trimmed[:maxBody]
		for len(trimmed) > 0 && !utf8.Valid(trimmed) {
			trimmed = trimmed[:len(trimmed)-1]
		}
	}
	return fmt.Errorf("claude: http %d: %s", status, strings.TrimSpace(string(trimmed)))
}

// maxRetryAfter caps the Retry-After we propagate. A misbehaving 429 server
// could send "Retry-After: 9999999" and we would otherwise attach a ~115-day
// duration to the typed error. Anthropic's documented 429 retry windows are
// minutes, and our agent timeouts cap at 12s anyway. Returning nil above
// the cap lets the Router fall back to its own backoff schedule rather than
// trusting an adversarial value (security-engineer review N2).
const maxRetryAfter = time.Hour

// parseRetryAfter parses an RFC 7231 Retry-After header. Returns nil for
// malformed input OR a non-positive computed duration OR a value larger
// than maxRetryAfter — the Router will then fall back to its own backoff
// schedule (code-architect Q6).
func parseRetryAfter(h string) *time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return nil
	}
	if n, err := strconv.Atoi(h); err == nil {
		if n <= 0 {
			return nil
		}
		d := time.Duration(n) * time.Second
		if d > maxRetryAfter {
			return nil
		}
		return &d
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d <= 0 || d > maxRetryAfter {
			return nil
		}
		return &d
	}
	return nil
}
