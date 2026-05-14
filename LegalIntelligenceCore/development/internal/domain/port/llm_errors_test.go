package port

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestLLMCodeCatalog_Exhaustive enforces the single-source-of-truth
// invariant: every LLMErrorCode in AllLLMErrorCodes() must have a policy
// in llmCodeCatalog, and the catalog must not contain entries that aren't
// declared. A drift fails the build before production.
func TestLLMCodeCatalog_Exhaustive(t *testing.T) {
	t.Parallel()
	declared := AllLLMErrorCodes()
	declaredSet := make(map[LLMErrorCode]struct{}, len(declared))
	for _, c := range declared {
		declaredSet[c] = struct{}{}
		if _, ok := llmCodeCatalog[c]; !ok {
			t.Errorf("declared code %q has no catalog row", c)
		}
	}
	for c := range llmCodeCatalog {
		if _, ok := declaredSet[c]; !ok {
			t.Errorf("catalog row %q has no declared constant", c)
		}
	}
}

// TestNewLLMProviderError_ReturnsCanonicalFlags asserts that the typed
// constructor sets Retryable / FallbackEligible per llm-provider-abstraction
// §1.2. A drift in policy here would silently change Router behaviour.
func TestNewLLMProviderError_ReturnsCanonicalFlags(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code             LLMErrorCode
		wantRetry        bool
		wantFallback     bool
	}{
		{LLMErrorTimeout, true, true},
		{LLMErrorRateLimit, true, true},
		{LLMErrorServerError, true, true},
		{LLMErrorNetwork, true, true},
		{LLMErrorOverloaded, true, true},
		{LLMErrorInvalidAPIKey, false, true},
		{LLMErrorQuotaExceeded, false, true},
		{LLMErrorContentPolicy, false, true},
		{LLMErrorContextTooLong, false, false},
		{LLMErrorMalformedRequest, false, false},
		{LLMErrorAllProvidersFailed, true, false},
	}
	for _, tc := range cases {
		got := NewLLMProviderError(tc.code, nil)
		if got == nil {
			t.Fatalf("NewLLMProviderError(%q) returned nil", tc.code)
		}
		if got.Retryable != tc.wantRetry || got.FallbackEligible != tc.wantFallback {
			t.Errorf("%q: got Retryable=%v Fallback=%v want %v / %v",
				tc.code, got.Retryable, got.FallbackEligible, tc.wantRetry, tc.wantFallback)
		}
	}
}

func TestNewLLMProviderError_UnknownReturnsNil(t *testing.T) {
	t.Parallel()
	if got := NewLLMProviderError(LLMErrorCode("UNDECLARED"), nil); got != nil {
		t.Fatalf("expected nil for undeclared code, got %v", got)
	}
}

func TestLLMProviderError_NilSafeHelpers(t *testing.T) {
	t.Parallel()
	var e *LLMProviderError
	if e.IsAuthError() || e.IsRetryable() || e.IsFallbackEligible() {
		t.Fatal("nil receiver must return false for all helpers")
	}
	if got := e.Error(); got != "<nil>" {
		t.Errorf("nil Error() = %q, want %q", got, "<nil>")
	}
	if e.Unwrap() != nil {
		t.Errorf("nil Unwrap() must return nil")
	}
}

func TestLLMProviderError_IsAuthError(t *testing.T) {
	t.Parallel()
	e := NewLLMProviderError(LLMErrorInvalidAPIKey, nil)
	if !e.IsAuthError() {
		t.Fatal("INVALID_API_KEY must report IsAuthError=true")
	}
	e2 := NewLLMProviderError(LLMErrorTimeout, nil)
	if e2.IsAuthError() {
		t.Fatal("TIMEOUT must NOT report IsAuthError=true")
	}
}

func TestLLMProviderError_ErrorIncludesWrappedDetail(t *testing.T) {
	t.Parallel()
	wrapped := errors.New("dial tcp 1.2.3.4:443: i/o timeout")
	e := NewLLMProviderError(LLMErrorNetwork, wrapped)
	msg := e.Error()
	for _, want := range []string{"NETWORK", "retryable=true", "fallback=true", "dial tcp"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing fragment %q", msg, want)
		}
	}
}

func TestAsLLMProviderError_ExtractsWrappedTypedError(t *testing.T) {
	t.Parallel()
	inner := NewLLMProviderError(LLMErrorOverloaded, nil)
	wrapped := fmt.Errorf("call failed: %w", inner)
	got, ok := AsLLMProviderError(wrapped)
	if !ok || got == nil {
		t.Fatalf("AsLLMProviderError did not unwrap")
	}
	if got.Code != LLMErrorOverloaded {
		t.Errorf("unwrapped code = %q, want %q", got.Code, LLMErrorOverloaded)
	}

	if _, ok := AsLLMProviderError(nil); ok {
		t.Error("AsLLMProviderError(nil) must return ok=false")
	}
	if _, ok := AsLLMProviderError(errors.New("other")); ok {
		t.Error("AsLLMProviderError on unrelated error must return ok=false")
	}
}

