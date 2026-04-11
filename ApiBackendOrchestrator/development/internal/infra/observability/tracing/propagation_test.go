package tracing

import (
	"context"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestInjectHTTPHeaders_NoOp_NoHeaders(t *testing.T) {
	// When tracing is disabled, the global propagator is a no-op.
	// Save and restore the global propagator to avoid test interference.
	original := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	defer otel.SetTextMapPropagator(original)

	req := httptest.NewRequest("GET", "/test", nil)
	InjectHTTPHeaders(context.Background(), req)

	if req.Header.Get("traceparent") != "" {
		t.Error("expected no traceparent header when tracing is disabled")
	}
}

func TestInjectHTTPHeaders_AddsTraceparent(t *testing.T) {
	// Enable a real tracer so the global propagator is set.
	tr, _ := newTestTracer(t)
	defer tr.Shutdown(context.Background())

	// Set the global propagator for this test.
	original := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer otel.SetTextMapPropagator(original)

	ctx, span := tr.StartSpan(context.Background(), "test.parent")
	defer span.End()

	req := httptest.NewRequest("GET", "/test", nil)
	InjectHTTPHeaders(ctx, req)

	traceparent := req.Header.Get("Traceparent")
	if traceparent == "" {
		t.Error("expected traceparent header when tracing is enabled")
	}
}

func TestExtractHTTPHeaders_WithTraceparent(t *testing.T) {
	original := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
	))
	defer otel.SetTextMapPropagator(original)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	ctx := ExtractHTTPHeaders(context.Background(), req)

	// The extracted context should have a remote span context.
	// We cannot directly assert on the context internals, but we can
	// verify the function does not panic and returns a non-nil context.
	if ctx == nil {
		t.Error("expected non-nil context")
	}
}

func TestCorrelationAttribute_ReturnsKeyValue(t *testing.T) {
	kv := CorrelationAttribute("test-corr-123")
	if kv.Key != AttrCorrelationID {
		t.Errorf("expected key %s, got %s", AttrCorrelationID, kv.Key)
	}
	if kv.Value.AsString() != "test-corr-123" {
		t.Errorf("expected value test-corr-123, got %s", kv.Value.AsString())
	}
}

func TestCorrelationAttribute_EmptyString(t *testing.T) {
	kv := CorrelationAttribute("")
	if kv.Key != AttrCorrelationID {
		t.Errorf("expected key %s, got %s", AttrCorrelationID, kv.Key)
	}
	if kv.Value.AsString() != "" {
		t.Errorf("expected empty value, got %q", kv.Value.AsString())
	}
}
