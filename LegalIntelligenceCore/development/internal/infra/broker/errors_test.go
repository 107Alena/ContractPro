package broker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestMapError_Nil(t *testing.T) {
	if mapError(nil, "Op") != nil {
		t.Error("mapError(nil) must be nil")
	}
}

func TestMapError_ContextErrorsPassThroughRaw(t *testing.T) {
	for _, ce := range []error{context.Canceled, context.DeadlineExceeded} {
		got := mapError(ce, "Publish")
		if !errors.Is(got, ce) {
			t.Errorf("want %v, got %v", ce, got)
		}
		var be *BrokerError
		if errors.As(got, &be) {
			t.Errorf("%v must not be wrapped in BrokerError", ce)
		}
	}
	// Wrapped context error also passes through.
	wrapped := fmt.Errorf("dial: %w", context.DeadlineExceeded)
	if be := (*BrokerError)(nil); errors.As(mapError(wrapped, "x"), &be) {
		t.Error("wrapped context error must pass through raw")
	}
}

func TestMapError_AMQPNonRetryableCodes(t *testing.T) {
	for _, code := range []int{404, 403, 406} {
		err := mapError(&amqp.Error{Code: code, Reason: "X"}, "Subscribe")
		if IsRetryable(err) {
			t.Errorf("AMQP %d must be non-retryable", code)
		}
		var be *BrokerError
		if !errors.As(err, &be) {
			t.Fatalf("AMQP %d must map to *BrokerError", code)
		}
	}
}

func TestMapError_AMQPRetryableCode(t *testing.T) {
	err := mapError(&amqp.Error{Code: 504, Reason: "CHANNEL_ERROR"}, "Publish")
	if !IsRetryable(err) {
		t.Error("AMQP 504 should be retryable")
	}
	if !errors.Is(err, err) || err.Error() == "" {
		t.Error("error string should be non-empty")
	}
}

func TestMapError_UnknownErrorRetryable(t *testing.T) {
	err := mapError(errors.New("connection reset by peer"), "Publish")
	if !IsRetryable(err) {
		t.Error("unknown/network error should be retryable by default")
	}
}

func TestBrokerError_UnwrapAndIs(t *testing.T) {
	be := &BrokerError{Op: "Publish", Retryable: true, Cause: ErrPublishNack}
	if !errors.Is(be, ErrPublishNack) {
		t.Error("errors.Is must traverse to the wrapped sentinel")
	}
	if be.Unwrap() != ErrPublishNack {
		t.Error("Unwrap must return Cause")
	}
	var nilBE *BrokerError
	if nilBE.Error() != "<nil>" {
		t.Error("nil BrokerError.Error() must be safe")
	}
	if nilBE.Unwrap() != nil {
		t.Error("nil BrokerError.Unwrap() must be nil")
	}
}

func TestRedactURLCredentials(t *testing.T) {
	url := "amqp://lic:s3cr3tPw@rabbit:5672/lic"

	// amqp.Dial-style error that echoes the full URI incl. password.
	in := errors.New(`dial amqp://lic:s3cr3tPw@rabbit:5672/lic: connection refused`)
	out := redactURLCredentials(in, url)
	if got := out.Error(); strings.Contains(got, "s3cr3tPw") {
		t.Fatalf("password leaked: %q", got)
	}
	if !strings.Contains(out.Error(), "***") {
		t.Errorf("expected redaction marker, got %q", out.Error())
	}

	// No password in message → original error (and wrapping) preserved.
	plain := errors.New("connection refused")
	if redactURLCredentials(plain, url) != plain {
		t.Error("error without the password must be returned unchanged")
	}
	// URL without credentials → unchanged.
	if redactURLCredentials(plain, "amqp://rabbit:5672/") != plain {
		t.Error("credential-free URL must leave error unchanged")
	}
	if redactURLCredentials(nil, url) != nil {
		t.Error("nil error → nil")
	}
}

func TestIsRetryable_NonBrokerError(t *testing.T) {
	if IsRetryable(errors.New("plain")) {
		t.Error("plain error must not be retryable")
	}
	if IsRetryable(context.Canceled) {
		t.Error("context.Canceled must not be retryable")
	}
	if IsRetryable(nil) {
		t.Error("nil must not be retryable")
	}
}
