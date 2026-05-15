package kvstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedisError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("connection reset")
	re := &RedisError{Op: "Get", Retryable: true, Cause: cause}

	if got, want := re.Error(), "kvstore: Get: connection reset"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(re, cause) {
		t.Error("errors.Is should traverse to Cause via Unwrap")
	}

	bare := &RedisError{Op: "Ping"}
	if got, want := bare.Error(), "kvstore: Ping"; got != want {
		t.Errorf("Error() (no cause) = %q, want %q", got, want)
	}

	var nilErr *RedisError
	if nilErr.Error() != "<nil>" {
		t.Errorf("nil RedisError.Error() = %q, want %q", nilErr.Error(), "<nil>")
	}
	if nilErr.Unwrap() != nil {
		t.Error("nil RedisError.Unwrap() should be nil")
	}
}

func TestIsRetryable(t *testing.T) {
	if !IsRetryable(&RedisError{Op: "Set", Retryable: true}) {
		t.Error("expected retryable")
	}
	if IsRetryable(&RedisError{Op: "Set", Retryable: false}) {
		t.Error("expected non-retryable")
	}
	if IsRetryable(fmt.Errorf("wrapped: %w", &RedisError{Op: "X", Retryable: true})) != true {
		t.Error("IsRetryable should unwrap")
	}
	if IsRetryable(errors.New("plain")) {
		t.Error("plain error is not retryable")
	}
	if IsRetryable(ErrKeyNotFound) {
		t.Error("ErrKeyNotFound is a plain sentinel, not retryable RedisError")
	}
}

func TestMapError_ContextPassthrough(t *testing.T) {
	for _, ce := range []error{context.Canceled, context.DeadlineExceeded} {
		got := mapError(ce, "Get")
		if !errors.Is(got, ce) {
			t.Errorf("context error not passed through raw: got %v", got)
		}
		var re *RedisError
		if errors.As(got, &re) {
			t.Errorf("context error must NOT be wrapped as *RedisError, got %v", got)
		}
	}
}

func TestMapError_RedisNil(t *testing.T) {
	got := mapError(redis.Nil, "Get")
	if !errors.Is(got, ErrKeyNotFound) {
		t.Errorf("redis.Nil should map to ErrKeyNotFound, got %v", got)
	}
}

func TestMapError_PoolClosedNonRetryable(t *testing.T) {
	got := mapError(redis.ErrClosed, "Set")
	if IsRetryable(got) {
		t.Error("redis.ErrClosed must map to a non-retryable error")
	}
	var re *RedisError
	if !errors.As(got, &re) || re.Retryable {
		t.Errorf("want non-retryable *RedisError, got %v", got)
	}
}

func TestMapError_UnknownIsRetryable(t *testing.T) {
	got := mapError(errors.New("i/o timeout"), "SetNX")
	if !IsRetryable(got) {
		t.Error("unknown/network error should be retryable by default")
	}
	var re *RedisError
	if !errors.As(got, &re) || re.Op != "SetNX" {
		t.Errorf("want *RedisError{Op:SetNX}, got %v", got)
	}
}

func TestMapError_Nil(t *testing.T) {
	if mapError(nil, "Get") != nil {
		t.Error("mapError(nil) should be nil")
	}
}

func TestErrClientClosed_NonRetryable(t *testing.T) {
	err := errClientClosed("Get")
	if IsRetryable(err) {
		t.Error("use-after-close must be non-retryable")
	}
	if !strings.Contains(err.Error(), "client closed") {
		t.Errorf("want 'client closed' in message, got %q", err.Error())
	}
}

func TestRedactURLCredentials(t *testing.T) {
	rawURL := "rediss://:s3cr3t-pw@redis.prod.internal:6380/2"

	in := errors.New("dial tcp: auth failed for rediss://:s3cr3t-pw@redis.prod.internal:6380/2")
	out := redactURLCredentials(in, rawURL)
	if strings.Contains(out.Error(), "s3cr3t-pw") {
		t.Errorf("password leaked: %q", out.Error())
	}
	if !strings.Contains(out.Error(), "***") {
		t.Errorf("expected redacted marker, got %q", out.Error())
	}

	// No password in URL → error returned unchanged.
	noPw := errors.New("boom")
	if got := redactURLCredentials(noPw, "redis://localhost:6379"); got != noPw {
		t.Error("error with no URL password should be returned unchanged")
	}

	// Password not present in message → wrapping preserved (same error value).
	other := errors.New("unrelated failure")
	if got := redactURLCredentials(other, rawURL); got != other {
		t.Error("when password absent from message, original error must be kept")
	}

	if redactURLCredentials(nil, rawURL) != nil {
		t.Error("nil in → nil out")
	}
}
