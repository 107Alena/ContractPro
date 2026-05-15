package gemini

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

// apiErrorEnvelope is Gemini's documented error JSON:
// {"error":{"code":<int>,"message":"...","status":"INVALID_ARGUMENT"}}.
// We try-decode it on any non-2xx response; when decode fails we fall back to
// the status code alone.
type apiErrorEnvelope struct {
	Error apiErrorDetail `json:"error"`
}

// apiErrorDetail is the inner Gemini error block. Status is the canonical
// machine token ("UNAUTHENTICATED", "PERMISSION_DENIED", "RESOURCE_EXHAUSTED",
// "INVALID_ARGUMENT", "FAILED_PRECONDITION", ...); Code is the numeric HTTP
// status mirror; Message is human-facing. We use Status + Message to refine
// the LLMErrorCode when the HTTP status alone is ambiguous.
type apiErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// mapTransportError translates errors returned by http.Client.Do (i.e. the
// call never made it to an HTTP status code) into a typed LLMProviderError.
//
// Every error returned to the Router MUST unwrap to *LLMProviderError so the
// Router's typed-switch decisions stay total (llm-provider-abstraction.md §1.2
// adapter invariant). Mirrors the claude/openai siblings.
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
// body so we can read Gemini's structured error envelope when present.
//
// There is no Anthropic-style 529 "overloaded" case for Gemini — that branch
// is intentionally absent. Gemini overloads 429 RESOURCE_EXHAUSTED for both
// transient rate limiting and hard quota exhaustion (split below). 403
// PERMISSION_DENIED is an auth/key-scope failure (valid key, API not enabled /
// no access) and is bucketed with 401 → InvalidAPIKey, matching the OpenAI
// sibling's 401||403 handling (code-architect MUST-FIX #6).
func mapHTTPError(status int, headers http.Header, body []byte) *port.LLMProviderError {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return port.NewLLMProviderError(port.LLMErrorInvalidAPIKey, httpStatusError(status, body))

	case status == http.StatusTooManyRequests:
		// RESOURCE_EXHAUSTED covers both transient rate limiting and hard
		// quota exhaustion. A quota-flavoured body must become QUOTA_EXCEEDED
		// so the Router marks the provider permanently unhealthy (~24h) and
		// falls back, rather than retrying a provider that will keep
		// rejecting.
		if env, ok := tryDecodeAPIError(body); ok && isQuotaExceeded(env.Error) {
			return port.NewLLMProviderError(port.LLMErrorQuotaExceeded, httpStatusError(status, body))
		}
		err := port.NewLLMProviderError(port.LLMErrorRateLimit, httpStatusError(status, body))
		if d := parseRetryAfter(headers.Get("Retry-After")); d != nil {
			err.RetryAfter = d
		}
		return err

	case status == http.StatusRequestTimeout:
		return port.NewLLMProviderError(port.LLMErrorTimeout, httpStatusError(status, body))

	case status >= 500:
		return port.NewLLMProviderError(port.LLMErrorServerError, httpStatusError(status, body))

	case status == http.StatusBadRequest:
		// Gemini, unlike OpenAI, does not use 422 — only 400 carries the
		// structured INVALID_ARGUMENT / FAILED_PRECONDITION family.
		return classify400(body, status)

	default:
		// Unknown status — default to SERVER_ERROR (retryable, fallback-
		// eligible) rather than MALFORMED. SERVER_ERROR lets the Router try
		// the same provider once and then move on, preserving fallback. A
		// MALFORMED default would lock out fallback for 451/421/etc. that
		// another provider could plausibly handle (mirrors claude/openai S9).
		return port.NewLLMProviderError(port.LLMErrorServerError, httpStatusError(status, body))
	}
}

// classify400 dispatches HTTP 400 by Gemini's structured error.status /
// .message. Falls back to LLMErrorMalformedRequest when the body cannot be
// decoded.
//
// Order matters — context-length and quota checks run before the generic
// content-policy / malformed buckets so a message that mentions "quota" or
// "token count" wins over a substring match on "policy"/"content" (mirrors
// the openai/claude classify4xx ordering).
func classify400(body []byte, status int) *port.LLMProviderError {
	env, ok := tryDecodeAPIError(body)
	if !ok {
		return port.NewLLMProviderError(port.LLMErrorMalformedRequest, httpStatusError(status, body))
	}
	if isContextLength(env.Error) {
		return port.NewLLMProviderError(port.LLMErrorContextTooLong, httpStatusError(status, body))
	}
	if isQuotaExceeded(env.Error) {
		return port.NewLLMProviderError(port.LLMErrorQuotaExceeded, httpStatusError(status, body))
	}
	if isContentPolicy(env.Error) {
		return port.NewLLMProviderError(port.LLMErrorContentPolicy, httpStatusError(status, body))
	}
	return port.NewLLMProviderError(port.LLMErrorMalformedRequest, httpStatusError(status, body))
}

// tryDecodeAPIError attempts to parse the Gemini error envelope. Returns
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
	if env.Error.Status == "" && env.Error.Message == "" && env.Error.Code == 0 {
		return env, false
	}
	return env, true
}

// isContentPolicy returns true when Gemini indicated the request was blocked
// for safety / policy reasons. Case-insensitive token matching guards against
// wire-side renames across API versions.
func isContentPolicy(d apiErrorDetail) bool {
	s := strings.ToLower(d.Status)
	if strings.Contains(s, "permission_denied") {
		// Note: 403 PERMISSION_DENIED is handled at the status level as an
		// auth error; this guards a 400 body that mislabels a safety block.
		return false
	}
	m := strings.ToLower(d.Message)
	return strings.Contains(m, "safety") || strings.Contains(m, "blocked") ||
		strings.Contains(m, "content policy") || strings.Contains(m, "prohibited") ||
		strings.Contains(m, "harm")
}

// isContextLength returns true when Gemini's error indicates the prompt
// exceeded the model's input/context window. The token-estimator + truncation
// pass (LIC-TASK-021) is meant to prevent this; when it slips through we
// surface it explicitly so the pipeline FAILs with is_retryable=false rather
// than burning fallback budget (no other provider has a materially larger
// window).
func isContextLength(d apiErrorDetail) bool {
	m := strings.ToLower(d.Message)
	if strings.Contains(m, "token") &&
		(strings.Contains(m, "exceed") || strings.Contains(m, "maximum") ||
			strings.Contains(m, "too long") || strings.Contains(m, "too many")) {
		return true
	}
	return strings.Contains(m, "context") && (strings.Contains(m, "too long") ||
		strings.Contains(m, "length") || strings.Contains(m, "exceeds"))
}

// isQuotaExceeded returns true when Gemini indicates a billing or quota
// constraint was hit (RESOURCE_EXHAUSTED with quota wording, or a 400 that
// mentions quota/billing). Routing it explicitly into LLMErrorQuotaExceeded
// preserves the catalog semantics (Router marks provider unhealthy ~24h).
func isQuotaExceeded(d apiErrorDetail) bool {
	m := strings.ToLower(d.Message)
	if strings.Contains(m, "quota") || strings.Contains(m, "billing") ||
		strings.Contains(m, "exceeded your current quota") {
		return true
	}
	// RESOURCE_EXHAUSTED alone is ambiguous (rate limit vs quota); only treat
	// it as quota when the message disambiguates, otherwise let the 429 path
	// keep it as a retryable RATE_LIMIT.
	return false
}

// maxErrorBodyBytes caps how many bytes of any provider-controlled error
// string we copy into our error chain (and thus, downstream, into structured
// logs). Bounding this is a 152-ФЗ / PII defense: a provider-controlled string
// could echo prompt fragments (contract text, which may contain ПДн). 512 B
// keeps the diagnostic context useful while preventing an unbounded body from
// landing in a single log line (mirrors the security-engineer review of the
// siblings).
const maxErrorBodyBytes = 512

// boundedDetail truncates a provider-controlled byte slice to
// maxErrorBodyBytes on a UTF-8 rune boundary so a non-ASCII payload cannot
// produce a corrupt sequence in our logs, then trims surrounding whitespace.
// The single chokepoint every provider-controlled error string passes through.
func boundedDetail(b []byte) string {
	trimmed := b
	if len(trimmed) > maxErrorBodyBytes {
		trimmed = trimmed[:maxErrorBodyBytes]
		for len(trimmed) > 0 && !utf8.Valid(trimmed) {
			trimmed = trimmed[:len(trimmed)-1]
		}
	}
	return strings.TrimSpace(string(trimmed))
}

// httpStatusError builds the wrapped underlying error attached to an
// LLMProviderError on the HTTP path. The body is truncated via boundedDetail
// to keep error strings bounded; Gemini's structured error payloads describe
// request shape, not document content, so PII is not normally present, but the
// cap is enforced regardless as defense in depth.
//
// The API key rides in the x-goog-api-key header, never the body or the URL —
// the canary test TestComplete_ErrorsDoNotLeakAPIKey asserts this invariant
// holds end-to-end across the HTTP and *url.Error paths.
func httpStatusError(status int, body []byte) error {
	return fmt.Errorf("gemini: http %d: %s", status, boundedDetail(body))
}

// maxRetryAfter caps the Retry-After we propagate. A misbehaving 429 server
// could send "Retry-After: 9999999" and we would otherwise attach a ~115-day
// duration to the typed error. Gemini's documented retry windows are
// seconds-to-minutes and agent timeouts cap low anyway. Returning nil above
// the cap lets the Router fall back to its own backoff schedule rather than
// trusting an adversarial value (mirrors the siblings).
const maxRetryAfter = time.Hour

// parseRetryAfter parses an RFC 7231 Retry-After header. Returns nil for
// malformed input OR a non-positive computed duration OR a value larger than
// maxRetryAfter — the Router then falls back to its own backoff schedule. A
// signed delta-seconds value (e.g. "+30" / "-5") is rejected: strconv.Atoi
// accepts a leading sign but a signed Retry-After is non-conformant and a
// negative one is nonsensical (mirrors the siblings). Gemini frequently omits
// Retry-After (carrying RetryInfo in error.details instead, which v1 does not
// parse); the nil return then defers to the Router's backoff.
func parseRetryAfter(h string) *time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return nil
	}
	if strings.HasPrefix(h, "+") || strings.HasPrefix(h, "-") {
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
