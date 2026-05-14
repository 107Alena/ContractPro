package claude

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
		name       string
		status     int
		body       string
		wantCode   port.LLMErrorCode
		retryable  bool
		fallback   bool
	}{
		{"401_unauthorized", 401, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`,
			port.LLMErrorInvalidAPIKey, false, true},
		{"403_forbidden", 403, `{"type":"error","error":{"type":"permission_error","message":"forbidden"}}`,
			port.LLMErrorInvalidAPIKey, false, true},
		{"429_rate_limit", 429, `{"type":"error","error":{"type":"rate_limit_error","message":"too many"}}`,
			port.LLMErrorRateLimit, true, true},
		{"408_timeout", 408, "", port.LLMErrorTimeout, true, true},
		{"500_server", 500, "internal error", port.LLMErrorServerError, true, true},
		{"502_bad_gateway", 502, "", port.LLMErrorServerError, true, true},
		{"503_unavailable", 503, "", port.LLMErrorServerError, true, true},
		{"529_overloaded", 529, "", port.LLMErrorOverloaded, true, true},
		{"400_content_policy_via_type", 400,
			`{"type":"error","error":{"type":"content_policy_violation","message":"blocked"}}`,
			port.LLMErrorContentPolicy, false, true},
		{"400_content_policy_via_message", 400,
			`{"type":"error","error":{"type":"invalid_request_error","message":"violates content policy rules"}}`,
			port.LLMErrorContentPolicy, false, true},
		{"400_context_too_long", 400,
			`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 200000 tokens > 200000 context length"}}`,
			port.LLMErrorContextTooLong, false, false},
		{"400_other_invalid", 400,
			`{"type":"error","error":{"type":"invalid_request_error","message":"missing model field"}}`,
			port.LLMErrorMalformedRequest, false, false},
		{"400_quota_via_type", 400,
			`{"type":"error","error":{"type":"billing_error","message":"plan limit"}}`,
			port.LLMErrorQuotaExceeded, false, true},
		{"400_quota_via_message", 400,
			`{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low"}}`,
			port.LLMErrorQuotaExceeded, false, true},
		{"400_unparsable_body", 400, "not-json",
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

func TestParseRetryAfter_AdversariallyLarge_ReturnsNil(t *testing.T) {
	// 9999999 seconds ≈ 115 days — well beyond maxRetryAfter (1h). Router
	// must use its own backoff schedule instead of trusting the value
	// (security-engineer review N2).
	got := parseRetryAfter("9999999")
	if got != nil {
		t.Fatalf("parseRetryAfter(99999999) = %v, want nil (above maxRetryAfter cap)", got)
	}
}

func TestParseRetryAfter_HTTPDate_FarFuture_ReturnsNil(t *testing.T) {
	farFuture := time.Now().Add(48 * time.Hour).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(farFuture)
	if got != nil {
		t.Fatalf("parseRetryAfter(48h-future) = %v, want nil (above cap)", got)
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
		{"Thu, 01 Jan 1970 00:00:00 GMT", nil}, // past date → non-positive
	}
	for _, c := range cases {
		got := parseRetryAfter(c.in)
		if got != c.want {
			t.Errorf("parseRetryAfter(%q)=%v, want %v", c.in, got, c.want)
		}
	}
	// Positive integer
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

type fakeNetError struct {
	timeout bool
	msg     string
}

func (e *fakeNetError) Error() string   { return e.msg }
func (e *fakeNetError) Timeout() bool   { return e.timeout }
func (e *fakeNetError) Temporary() bool { return false }
