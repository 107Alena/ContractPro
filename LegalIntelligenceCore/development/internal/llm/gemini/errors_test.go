package gemini

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestMapTransportError(t *testing.T) {
	if lpe := mapTransportError(context.DeadlineExceeded); lpe.Code != port.LLMErrorTimeout {
		t.Errorf("DeadlineExceeded → %v, want TIMEOUT", lpe.Code)
	}
	if lpe := mapTransportError(context.Canceled); lpe.Code != port.LLMErrorTimeout {
		t.Errorf("Canceled → %v, want TIMEOUT", lpe.Code)
	}
	if lpe := mapTransportError(errors.New("dial tcp: connection refused")); lpe.Code != port.LLMErrorNetwork {
		t.Errorf("generic → %v, want NETWORK", lpe.Code)
	}
	if mapTransportError(nil) != nil {
		t.Errorf("nil → want nil")
	}
}

func TestMapHTTPError_StatusMatrix(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   port.LLMErrorCode
	}{
		{401, `{"error":{"status":"UNAUTHENTICATED"}}`, port.LLMErrorInvalidAPIKey},
		{403, `{"error":{"status":"PERMISSION_DENIED"}}`, port.LLMErrorInvalidAPIKey},
		{408, ``, port.LLMErrorTimeout},
		{429, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"rate limit"}}`, port.LLMErrorRateLimit},
		{429, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"exceeded your current quota"}}`, port.LLMErrorQuotaExceeded},
		{500, ``, port.LLMErrorServerError},
		{503, ``, port.LLMErrorServerError},
		{418, ``, port.LLMErrorServerError}, // unknown → SERVER_ERROR (preserves fallback)
		{400, `{"error":{"message":"Invalid JSON payload","status":"INVALID_ARGUMENT"}}`, port.LLMErrorMalformedRequest},
		{400, `{"error":{"message":"input token count exceeds the maximum","status":"INVALID_ARGUMENT"}}`, port.LLMErrorContextTooLong},
		{400, `{"error":{"message":"request blocked by safety filters","status":"INVALID_ARGUMENT"}}`, port.LLMErrorContentPolicy},
		{400, `not-json`, port.LLMErrorMalformedRequest},
	}
	for _, c := range cases {
		lpe := mapHTTPError(c.status, http.Header{}, []byte(c.body))
		if lpe.Code != c.want {
			t.Errorf("mapHTTPError(%d,%q) = %v, want %v", c.status, c.body, lpe.Code, c.want)
		}
	}
}

func TestMapHTTPError_429RetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "45")
	lpe := mapHTTPError(429, h, []byte(`{"error":{"status":"RESOURCE_EXHAUSTED"}}`))
	if lpe.Code != port.LLMErrorRateLimit {
		t.Fatalf("Code=%v", lpe.Code)
	}
	if lpe.RetryAfter == nil || *lpe.RetryAfter != 45*time.Second {
		t.Errorf("RetryAfter=%v, want 45s", lpe.RetryAfter)
	}
}

func TestMapHTTPError_CatalogFlagsConsistent(t *testing.T) {
	// Every adapter-produced code must carry the canonical (Retryable,
	// FallbackEligible) pair from the port catalog.
	for _, c := range []struct {
		status int
		body   string
	}{
		{401, ``}, {403, ``}, {429, ``}, {500, ``}, {400, `{"error":{"message":"x token exceeds maximum","status":"INVALID_ARGUMENT"}}`},
	} {
		lpe := mapHTTPError(c.status, http.Header{}, []byte(c.body))
		canon := port.NewLLMProviderError(lpe.Code, nil)
		if lpe.Retryable != canon.Retryable || lpe.FallbackEligible != canon.FallbackEligible {
			t.Errorf("status %d code %v flags=(%t,%t), want catalog (%t,%t)",
				c.status, lpe.Code, lpe.Retryable, lpe.FallbackEligible, canon.Retryable, canon.FallbackEligible)
		}
	}
}

func TestTryDecodeAPIError(t *testing.T) {
	if _, ok := tryDecodeAPIError(nil); ok {
		t.Errorf("nil body → ok=true, want false")
	}
	if _, ok := tryDecodeAPIError([]byte(`{}`)); ok {
		t.Errorf("empty envelope → ok=true, want false")
	}
	env, ok := tryDecodeAPIError([]byte(`{"error":{"code":400,"message":"m","status":"INVALID_ARGUMENT"}}`))
	if !ok || env.Error.Status != "INVALID_ARGUMENT" {
		t.Errorf("decode = (%+v,%v)", env, ok)
	}
}

func TestParseRetryAfter(t *testing.T) {
	d := parseRetryAfter("30")
	if d == nil || *d != 30*time.Second {
		t.Errorf("'30' → %v, want 30s", d)
	}
	if parseRetryAfter("") != nil {
		t.Errorf("'' → want nil")
	}
	if parseRetryAfter("+30") != nil {
		t.Errorf("'+30' (signed) → want nil")
	}
	if parseRetryAfter("-5") != nil {
		t.Errorf("'-5' → want nil")
	}
	if parseRetryAfter("0") != nil {
		t.Errorf("'0' → want nil")
	}
	if parseRetryAfter("99999999") != nil {
		t.Errorf("over-cap → want nil")
	}
	if parseRetryAfter("garbage") != nil {
		t.Errorf("garbage → want nil")
	}
	if d := parseRetryAfter(time.Now().Add(20 * time.Second).UTC().Format(http.TimeFormat)); d == nil || *d <= 0 {
		t.Errorf("HTTP-date → %v, want positive", d)
	}
}

func TestBoundedDetail_RuneBoundaryAndCap(t *testing.T) {
	if got := boundedDetail([]byte("  hello  ")); got != "hello" {
		t.Errorf("trim: got %q", got)
	}
	long := strings.Repeat("я", 1000) // 2 bytes/rune → 2000 bytes
	got := boundedDetail([]byte(long))
	if len(got) > maxErrorBodyBytes {
		t.Errorf("len=%d, want <= %d", len(got), maxErrorBodyBytes)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatalf("truncation produced an invalid rune (not rune-boundary safe)")
		}
	}
}

func TestHTTPStatusError_BoundedAndPrefixed(t *testing.T) {
	err := httpStatusError(503, []byte(strings.Repeat("x", 5000)))
	if !strings.HasPrefix(err.Error(), "gemini: http 503: ") {
		t.Errorf("prefix wrong: %q", err.Error()[:25])
	}
	if len(err.Error()) > len("gemini: http 503: ")+maxErrorBodyBytes {
		t.Errorf("error string not bounded: len=%d", len(err.Error()))
	}
}
