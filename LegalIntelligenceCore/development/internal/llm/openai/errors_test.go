package openai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestMapHTTPError_StatusCodeMatrix(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantCode  port.LLMErrorCode
		retryable bool
		fallback  bool
	}{
		{"401_unauthorized", 401, `{"error":{"type":"invalid_request_error","code":"invalid_api_key","message":"Incorrect API key"}}`,
			port.LLMErrorInvalidAPIKey, false, true},
		{"403_forbidden", 403, `{"error":{"type":"invalid_request_error","message":"forbidden"}}`,
			port.LLMErrorInvalidAPIKey, false, true},
		{"429_rate_limit", 429, `{"error":{"type":"requests","code":"rate_limit_exceeded","message":"Rate limit reached"}}`,
			port.LLMErrorRateLimit, true, true},
		{"429_insufficient_quota", 429, `{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"You exceeded your current quota"}}`,
			port.LLMErrorQuotaExceeded, false, true},
		{"408_timeout", 408, "", port.LLMErrorTimeout, true, true},
		{"500_server", 500, "internal error", port.LLMErrorServerError, true, true},
		{"502_bad_gateway", 502, "", port.LLMErrorServerError, true, true},
		{"503_unavailable", 503, "", port.LLMErrorServerError, true, true},
		{"400_content_policy_via_code", 400,
			`{"error":{"type":"invalid_request_error","code":"content_policy_violation","message":"rejected"}}`,
			port.LLMErrorContentPolicy, false, true},
		{"400_content_policy_via_message", 400,
			`{"error":{"type":"invalid_request_error","message":"Your request was rejected by the content policy"}}`,
			port.LLMErrorContentPolicy, false, true},
		{"400_context_too_long_via_code", 400,
			`{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"maximum context length is 128000 tokens"}}`,
			port.LLMErrorContextTooLong, false, false},
		{"422_context_too_long_via_message", 422,
			`{"error":{"type":"invalid_request_error","message":"This model's maximum context length is 128000 tokens, however you requested 200000"}}`,
			port.LLMErrorContextTooLong, false, false},
		{"400_other_invalid", 400,
			`{"error":{"type":"invalid_request_error","message":"Missing required parameter: 'model'"}}`,
			port.LLMErrorMalformedRequest, false, false},
		{"400_quota_via_code", 400,
			`{"error":{"type":"invalid_request_error","code":"insufficient_quota","message":"plan limit"}}`,
			port.LLMErrorQuotaExceeded, false, true},
		{"400_quota_via_message", 400,
			`{"error":{"type":"invalid_request_error","message":"You exceeded your current quota, please check your billing"}}`,
			port.LLMErrorQuotaExceeded, false, true},
		{"400_unparsable_body", 400, "not-json",
			port.LLMErrorMalformedRequest, false, false},
		// Valid JSON but an empty error object → no usable discriminators →
		// status-code-only fallback (MALFORMED for 400). golang-pro S3.4.
		{"400_empty_error_object", 400, `{"error":{}}`,
			port.LLMErrorMalformedRequest, false, false},
		// Unknown status defaults to SERVER_ERROR so the Router preserves
		// retry + fallback semantics; locking to MALFORMED would deny both
		// for a 451/421/etc. another provider could plausibly handle.
		{"418_unknown", 418, "weird", port.LLMErrorServerError, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mapHTTPError(tc.status, http.Header{}, []byte(tc.body))
			if err == nil {
				t.Fatalf("got nil error")
			}
			if err.Code != tc.wantCode {
				t.Errorf("Code=%v, want %v", err.Code, tc.wantCode)
			}
			if err.Retryable != tc.retryable {
				t.Errorf("Retryable=%v, want %v", err.Retryable, tc.retryable)
			}
			if err.FallbackEligible != tc.fallback {
				t.Errorf("FallbackEligible=%v, want %v", err.FallbackEligible, tc.fallback)
			}
		})
	}
}

func TestMapHTTPError_NoAnthropic529Branch(t *testing.T) {
	// 529 is Anthropic-specific; for OpenAI it must fall through to the
	// unknown-status SERVER_ERROR default, never LLMErrorOverloaded.
	err := mapHTTPError(529, http.Header{}, []byte("overloaded"))
	if err.Code == port.LLMErrorOverloaded {
		t.Fatalf("529 mapped to LLMErrorOverloaded; OpenAI has no overloaded code")
	}
	if err.Code != port.LLMErrorServerError {
		t.Errorf("Code=%v, want LLMErrorServerError (unknown-status default)", err.Code)
	}
}

func TestMapHTTPError_429_WithRetryAfterHeader_IntegerSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "12")
	err := mapHTTPError(429, h, []byte(`{}`))
	if err.RetryAfter == nil {
		t.Fatalf("RetryAfter is nil; expected 12s")
	}
	if *err.RetryAfter != 12*time.Second {
		t.Errorf("RetryAfter=%v, want 12s", *err.RetryAfter)
	}
}

func TestMapHTTPError_429_WithRetryAfterHeader_HTTPDate(t *testing.T) {
	h := http.Header{}
	future := time.Now().Add(45 * time.Second).UTC().Format(http.TimeFormat)
	h.Set("Retry-After", future)
	err := mapHTTPError(429, h, []byte(`{}`))
	if err.RetryAfter == nil {
		t.Fatalf("RetryAfter is nil; expected ~45s")
	}
	if *err.RetryAfter <= 0 {
		t.Errorf("RetryAfter=%v, want positive", *err.RetryAfter)
	}
}

func TestMapHTTPError_429_NoRetryAfter_LeavesNil(t *testing.T) {
	err := mapHTTPError(429, http.Header{}, []byte(`{}`))
	if err.RetryAfter != nil {
		t.Fatalf("RetryAfter=%v, want nil (Router applies its own backoff)", *err.RetryAfter)
	}
}

func TestMapHTTPError_429_QuotaTakesPrecedenceOverRetryAfter(t *testing.T) {
	// An insufficient_quota 429 is permanent — it must map to QUOTA_EXCEEDED
	// (Retryable=false) and NOT carry a RetryAfter that would invite a retry.
	h := http.Header{}
	h.Set("Retry-After", "30")
	err := mapHTTPError(429, h, []byte(`{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"quota"}}`))
	if err.Code != port.LLMErrorQuotaExceeded {
		t.Fatalf("Code=%v, want QuotaExceeded", err.Code)
	}
	if err.Retryable {
		t.Errorf("QUOTA_EXCEEDED must be Retryable=false; got %+v", err)
	}
	if err.RetryAfter != nil {
		t.Errorf("quota error must not carry RetryAfter; got %v", *err.RetryAfter)
	}
}

func TestParseRetryAfter_AdversariallyLarge_ReturnsNil(t *testing.T) {
	got := parseRetryAfter("9999999")
	if got != nil {
		t.Fatalf("parseRetryAfter(9999999) = %v, want nil (above maxRetryAfter cap)", got)
	}
}

func TestParseRetryAfter_HTTPDate_FarFuture_ReturnsNil(t *testing.T) {
	farFuture := time.Now().Add(48 * time.Hour).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(farFuture)
	if got != nil {
		t.Fatalf("parseRetryAfter(48h-future) = %v, want nil (above cap)", got)
	}
}

func TestParseRetryAfter_RejectsSignedDeltaSeconds(t *testing.T) {
	// strconv.Atoi accepts a leading sign, but a signed Retry-After is
	// non-conformant (RFC 7231 delta-seconds is unsigned). Both must be nil.
	for _, in := range []string{"+30", "-30"} {
		if got := parseRetryAfter(in); got != nil {
			t.Errorf("parseRetryAfter(%q)=%v, want nil (signed rejected)", in, got)
		}
	}
}

func TestParseRetryAfter_Variants(t *testing.T) {
	cases := []struct {
		in   string
		want *time.Duration
	}{
		{"", nil},
		{"   ", nil},
		{"0", nil},
		{"-5", nil},
		{"garbage", nil},
		{"Thu, 01 Jan 1970 00:00:00 GMT", nil},
	}
	for _, c := range cases {
		got := parseRetryAfter(c.in)
		if got != c.want {
			t.Errorf("parseRetryAfter(%q)=%v, want %v", c.in, got, c.want)
		}
	}
	if d := parseRetryAfter("3"); d == nil || *d != 3*time.Second {
		t.Errorf("parseRetryAfter(\"3\")=%v, want 3s", d)
	}
}

func TestMapTransportError_ContextDeadline_AsTimeout(t *testing.T) {
	err := mapTransportError(context.DeadlineExceeded)
	if err == nil || err.Code != port.LLMErrorTimeout {
		t.Fatalf("err=%v, want LLMErrorTimeout", err)
	}
	if !err.Retryable || !err.FallbackEligible {
		t.Errorf("timeout must be Retryable + FallbackEligible, got %+v", err)
	}
}

func TestMapTransportError_ContextCanceled_AsTimeout(t *testing.T) {
	err := mapTransportError(context.Canceled)
	if err == nil || err.Code != port.LLMErrorTimeout {
		t.Fatalf("err=%v, want LLMErrorTimeout", err)
	}
}

func TestMapTransportError_WrappedDeadline(t *testing.T) {
	wrapped := fmt.Errorf("post: %w", context.DeadlineExceeded)
	err := mapTransportError(wrapped)
	if err.Code != port.LLMErrorTimeout {
		t.Errorf("Code=%v, want LLMErrorTimeout (wrapped)", err.Code)
	}
	if !errors.Is(err.Unwrap(), context.DeadlineExceeded) {
		t.Errorf("Unwrap chain broken; errors.Is should still find DeadlineExceeded")
	}
}

func TestMapTransportError_NetTimeout_AsTimeout(t *testing.T) {
	err := mapTransportError(&fakeNetError{timeout: true, msg: "i/o timeout"})
	if err.Code != port.LLMErrorTimeout {
		t.Errorf("Code=%v, want LLMErrorTimeout", err.Code)
	}
}

func TestMapTransportError_NetNonTimeout_AsNetwork(t *testing.T) {
	err := mapTransportError(&net.DNSError{Err: "no such host", Name: "x"})
	if err.Code != port.LLMErrorNetwork {
		t.Errorf("Code=%v, want LLMErrorNetwork", err.Code)
	}
	if !err.Retryable || !err.FallbackEligible {
		t.Errorf("network must be Retryable + FallbackEligible, got %+v", err)
	}
}

func TestMapTransportError_GenericError_AsNetwork(t *testing.T) {
	err := mapTransportError(errors.New("EOF"))
	if err.Code != port.LLMErrorNetwork {
		t.Errorf("Code=%v, want LLMErrorNetwork", err.Code)
	}
}

func TestMapTransportError_Nil(t *testing.T) {
	if got := mapTransportError(nil); got != nil {
		t.Errorf("mapTransportError(nil)=%v, want nil", got)
	}
}

func TestHTTPStatusError_TruncatesLargeBody(t *testing.T) {
	huge := strings.Repeat("x", 2048)
	err := httpStatusError(503, []byte(huge))
	if len(err.Error()) > 600 {
		t.Errorf("error too long (%d chars); expected <600 (truncation broken)", len(err.Error()))
	}
}

func TestHTTPStatusError_TruncatesOnUTF8Boundary(t *testing.T) {
	// 600 copies of the 3-byte rune 中 (1800 bytes) — the 512-byte cut lands
	// mid-rune; truncation must back off to a valid UTF-8 boundary so the
	// logged error is not a corrupt byte sequence.
	body := []byte(strings.Repeat("中", 600))
	err := httpStatusError(500, body)
	msg := err.Error()
	idx := strings.Index(msg, "中")
	if idx < 0 {
		t.Fatalf("expected at least one valid 中 rune in %q", msg)
	}
	for _, r := range msg[idx:] {
		if r == '�' {
			t.Fatalf("error contains UTF-8 replacement char; truncation cut mid-rune: %q", msg)
		}
	}
}

type fakeNetError struct {
	timeout bool
	msg     string
}

func (e *fakeNetError) Error() string   { return e.msg }
func (e *fakeNetError) Timeout() bool   { return e.timeout }
func (e *fakeNetError) Temporary() bool { return false }
