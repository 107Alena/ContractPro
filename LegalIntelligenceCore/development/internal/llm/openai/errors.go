package openai

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

// apiErrorEnvelope is OpenAI's documented error JSON. Unlike Anthropic there
// is no top-level `type` — the discriminators live entirely inside `error`.
// We try-decode it on any non-2xx response; when decode fails we fall back to
// the status code alone.
type apiErrorEnvelope struct {
	Error apiErrorDetail `json:"error"`
}

// apiErrorDetail is the inner OpenAI error block. Code is the canonical
// machine token ("insufficient_quota", "context_length_exceeded",
// "content_policy_violation", ...); Type is the broad family
// ("invalid_request_error", "rate_limit_error", ...). We use both to refine
// the LLMErrorCode when the HTTP status alone is ambiguous. `param` is
// intentionally NOT decoded — nothing reads it (golang-pro N1).
type apiErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// mapTransportError translates errors returned by http.Client.Do (i.e. the
// call never made it to an HTTP status code) into a typed LLMProviderError.
//
// Every error returned to the Router MUST unwrap to *LLMProviderError so the
// Router's typed-switch decisions stay total (llm-provider-abstraction.md §1.2
// adapter invariant). Mirrors the Claude sibling's mapTransportError.
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
// body so we can read OpenAI's structured error envelope when present.
//
// There is no Anthropic-style 529 "overloaded" case for OpenAI — that branch
// is intentionally absent.
func mapHTTPError(status int, headers http.Header, body []byte) *port.LLMProviderError {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return port.NewLLMProviderError(port.LLMErrorInvalidAPIKey, httpStatusError(status, body))

	case status == http.StatusTooManyRequests:
		// OpenAI overloads 429 for both transient rate limiting and hard quota
		// exhaustion. insufficient_quota must become QUOTA_EXCEEDED so the
		// Router marks the provider permanently unhealthy and falls back,
		// rather than retrying a provider that will keep rejecting.
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

	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return classify4xx(body, status)

	default:
		// Unknown status — default to SERVER_ERROR (retryable, fallback-
		// eligible) rather than MALFORMED. SERVER_ERROR lets the Router try
		// the same provider once and then move on, preserving fallback. A
		// MALFORMED default would lock out fallback for 451/421/etc. that
		// another provider could plausibly handle (mirrors Claude S9).
		return port.NewLLMProviderError(port.LLMErrorServerError, httpStatusError(status, body))
	}
}

// classify4xx dispatches HTTP 400 / 422 by OpenAI's structured error.code /
// .type / .message. Falls back to LLMErrorMalformedRequest when the body
// cannot be decoded.
//
// Order matters — context-length and quota checks run before the generic
// content-policy / malformed buckets so a message that mentions "quota" or
// "context length" wins over a substring match on "policy"/"content".
func classify4xx(body []byte, status int) *port.LLMProviderError {
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

// tryDecodeAPIError attempts to parse the OpenAI error envelope. Returns
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
	if env.Error.Type == "" && env.Error.Message == "" && env.Error.Code == "" {
		return env, false
	}
	return env, true
}

// isContentPolicy returns true when OpenAI indicated the request was blocked
// for policy reasons. The exact token has shifted across API versions — we
// accept any case-insensitive match of well-known tokens rather than a single
// literal so a wire-side rename does not break us.
func isContentPolicy(d apiErrorDetail) bool {
	c := strings.ToLower(d.Code)
	if strings.Contains(c, "content_policy") || strings.Contains(c, "content_filter") {
		return true
	}
	t := strings.ToLower(d.Type)
	if strings.Contains(t, "content_policy") || strings.Contains(t, "policy_violation") {
		return true
	}
	m := strings.ToLower(d.Message)
	return strings.Contains(m, "content policy") || strings.Contains(m, "content_policy_violation")
}

// isContextLength returns true when OpenAI's error indicates the prompt
// exceeded the model's context window. The token-estimator + truncation pass
// (LIC-TASK-021) is meant to prevent this; when it slips through we surface it
// explicitly so the pipeline FAILs with is_retryable=false rather than burning
// fallback budget (no other provider has a materially larger window).
func isContextLength(d apiErrorDetail) bool {
	c := strings.ToLower(d.Code)
	if strings.Contains(c, "context_length_exceeded") || strings.Contains(c, "context_length") {
		return true
	}
	m := strings.ToLower(d.Message)
	return strings.Contains(m, "context") && (strings.Contains(m, "too long") ||
		strings.Contains(m, "length") || strings.Contains(m, "maximum context") ||
		strings.Contains(m, "exceeds"))
}

// isQuotaExceeded returns true when OpenAI indicates a billing or quota
// constraint was hit. The canonical code is "insufficient_quota" (often a 429,
// occasionally a 400); routing it explicitly into LLMErrorQuotaExceeded
// preserves the catalog semantics (Router marks provider unhealthy ~24h).
func isQuotaExceeded(d apiErrorDetail) bool {
	c := strings.ToLower(d.Code)
	if strings.Contains(c, "insufficient_quota") || strings.Contains(c, "quota") ||
		strings.Contains(c, "billing") {
		return true
	}
	t := strings.ToLower(d.Type)
	if strings.Contains(t, "insufficient_quota") || strings.Contains(t, "quota") {
		return true
	}
	m := strings.ToLower(d.Message)
	return strings.Contains(m, "quota") || strings.Contains(m, "billing") ||
		strings.Contains(m, "exceeded your current quota")
}

// maxErrorBodyBytes caps how many bytes of any provider-controlled error
// string we copy into our error chain (and thus, downstream, into structured
// logs). Bounding this is a 152-ФЗ / PII defense: a provider-controlled string
// — whether an HTTP error body or an inline status:"failed" generation message
// — could echo prompt fragments (contract text, which may contain ПДн).
// 512 B keeps the diagnostic context useful while preventing an unbounded
// body from landing in a single log line (security-engineer review S1).
const maxErrorBodyBytes = 512

// boundedDetail truncates a provider-controlled byte slice to
// maxErrorBodyBytes on a UTF-8 rune boundary so a non-ASCII payload cannot
// produce a corrupt sequence in our logs, then trims surrounding whitespace.
// The single chokepoint every provider-controlled string MUST pass through.
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

// boundedErrorMessage is the string-input convenience over boundedDetail, used
// by the parseResponse status:"failed" path where the provider-controlled
// message arrives already decoded (security-engineer review S1).
func boundedErrorMessage(s string) string {
	return boundedDetail([]byte(s))
}

// httpStatusError builds the wrapped underlying error attached to an
// LLMProviderError on the HTTP path. The body is truncated via boundedDetail
// to keep error strings bounded; OpenAI's structured error payloads describe
// request shape, not document content, so PII is not normally present, but the
// cap is enforced regardless as defense in depth.
//
// The API key rides in the Authorization header, never the body — but the
// canary test TestComplete_ErrorsDoNotLeakAPIKey asserts this invariant holds
// end-to-end across the HTTP, status:"failed", and *url.Error paths.
func httpStatusError(status int, body []byte) error {
	return fmt.Errorf("openai: http %d: %s", status, boundedDetail(body))
}

// maxRetryAfter caps the Retry-After we propagate. A misbehaving 429 server
// could send "Retry-After: 9999999" and we would otherwise attach a ~115-day
// duration to the typed error. OpenAI's documented 429 retry windows are
// seconds-to-minutes, and our agent timeouts cap low anyway. Returning nil
// above the cap lets the Router fall back to its own backoff schedule rather
// than trusting an adversarial value (mirrors Claude maxRetryAfter).
const maxRetryAfter = time.Hour

// parseRetryAfter parses an RFC 7231 Retry-After header. Returns nil for
// malformed input OR a non-positive computed duration OR a value larger than
// maxRetryAfter — the Router then falls back to its own backoff schedule. A
// signed delta-seconds value (e.g. "+30" / "-5") is rejected: strconv.Atoi
// accepts a leading sign but a signed Retry-After is non-conformant and a
// negative one is nonsensical (mirrors Claude parseRetryAfter).
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
