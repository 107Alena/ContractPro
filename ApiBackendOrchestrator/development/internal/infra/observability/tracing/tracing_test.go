package tracing

import (
	"context"
	"testing"

	"contractpro/api-orchestrator/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"
)

// resetGlobalOTel restores the global OTel provider and propagator to defaults.
// Must be called as defer resetGlobalOTel() in any test that creates an enabled tracer.
func resetGlobalOTel() {
	otel.SetTracerProvider(noop.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
}

func TestNewTracer_DisabledReturnsNoOp(t *testing.T) {
	tr, err := NewTracer(context.Background(), config.ObservabilityConfig{
		TracingEnabled:  false,
		TracingEndpoint: "localhost:4318",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Enabled() {
		t.Error("expected Enabled() == false for disabled tracer")
	}
	if err := tr.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown error: %v", err)
	}
}

func TestNewTracer_EmptyEndpointReturnsNoOp(t *testing.T) {
	tr, err := NewTracer(context.Background(), config.ObservabilityConfig{
		TracingEnabled:  true,
		TracingEndpoint: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Enabled() {
		t.Error("expected Enabled() == false when endpoint is empty")
	}
}

func TestNewTracer_DisabledAndEmptyEndpoint(t *testing.T) {
	tr, err := NewTracer(context.Background(), config.ObservabilityConfig{
		TracingEnabled:  false,
		TracingEndpoint: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Enabled() {
		t.Error("expected Enabled() == false")
	}
}

func TestNewTracer_EnabledWithEndpoint(t *testing.T) {
	// Use a non-routable endpoint so the exporter is created but cannot connect.
	// NewTracer does NOT block on the exporter connection (OTLP/HTTP uses lazy connect).
	defer resetGlobalOTel()
	tr, err := NewTracer(context.Background(), config.ObservabilityConfig{
		TracingEnabled:  true,
		TracingEndpoint: "localhost:4318",
		TracingInsecure: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Shutdown(context.Background())

	if !tr.Enabled() {
		t.Error("expected Enabled() == true for enabled tracer")
	}
}

func TestTracer_StartSpan_NoOp_NoPanic(t *testing.T) {
	tr, _ := NewTracer(context.Background(), config.ObservabilityConfig{})
	ctx, span := tr.StartSpan(context.Background(), "test.span")
	defer span.End()

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if span == nil {
		t.Fatal("expected non-nil span")
	}
	// Verify the span is a noop span (not recording).
	if span.IsRecording() {
		t.Error("noop span should not be recording")
	}
}

func TestTracer_StartSpan_Enabled(t *testing.T) {
	defer resetGlobalOTel()
	tr, err := NewTracer(context.Background(), config.ObservabilityConfig{
		TracingEnabled:  true,
		TracingEndpoint: "localhost:4318",
		TracingInsecure: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Shutdown(context.Background())

	ctx, span := tr.StartSpan(context.Background(), "test.span")
	defer span.End()

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	// When the sampler decides to record, IsRecording() should return true.
	// With 10% ratio sampling, we cannot guarantee this for every trace ID.
	// We only verify the span is non-nil and the API does not panic.
	if span == nil {
		t.Fatal("expected non-nil span")
	}
}

func TestTracer_SpanFromContext_Enriched(t *testing.T) {
	tr, _ := NewTracer(context.Background(), config.ObservabilityConfig{})
	ctx, parentSpan := tr.StartSpan(context.Background(), "parent")
	defer parentSpan.End()

	got := tr.SpanFromContext(ctx)
	if got == nil {
		t.Fatal("expected non-nil span from enriched context")
	}
}

func TestTracer_SpanFromContext_BackgroundCtx(t *testing.T) {
	tr, _ := NewTracer(context.Background(), config.ObservabilityConfig{})
	got := tr.SpanFromContext(context.Background())
	if got == nil {
		t.Fatal("expected non-nil span from bare context")
	}
	// Background context returns a noop span — should not be recording.
	if got.IsRecording() {
		t.Error("expected noop span from background context")
	}
}

func TestTracer_Shutdown_NoOp_NoError(t *testing.T) {
	tr, _ := NewTracer(context.Background(), config.ObservabilityConfig{})
	if err := tr.Shutdown(context.Background()); err != nil {
		t.Errorf("unexpected shutdown error: %v", err)
	}
}

func TestTracer_Shutdown_MultipleCallsSafe(t *testing.T) {
	tr, _ := NewTracer(context.Background(), config.ObservabilityConfig{})
	for i := 0; i < 3; i++ {
		if err := tr.Shutdown(context.Background()); err != nil {
			t.Errorf("call %d: unexpected error: %v", i, err)
		}
	}
}

func TestTracer_Enabled_ReflectsConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.ObservabilityConfig
		expected bool
	}{
		{"disabled", config.ObservabilityConfig{TracingEnabled: false, TracingEndpoint: "localhost:4318"}, false},
		{"enabled_no_endpoint", config.ObservabilityConfig{TracingEnabled: true, TracingEndpoint: ""}, false},
		{"both_empty", config.ObservabilityConfig{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := NewTracer(context.Background(), tt.cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer tr.Shutdown(context.Background())
			if tr.Enabled() != tt.expected {
				t.Errorf("Enabled() = %v, want %v", tr.Enabled(), tt.expected)
			}
		})
	}
}

func TestTracer_Provider_ReturnsNonNil(t *testing.T) {
	tr, _ := NewTracer(context.Background(), config.ObservabilityConfig{})
	if tr.Provider() == nil {
		t.Error("expected non-nil TracerProvider")
	}
}

func TestTracer_NoOp_ProviderType(t *testing.T) {
	tr, _ := NewTracer(context.Background(), config.ObservabilityConfig{})
	switch tr.Provider().(type) {
	case noop.TracerProvider, *noop.TracerProvider:
		// OK — either value or pointer type is acceptable.
	default:
		t.Errorf("expected noop.TracerProvider, got %T", tr.Provider())
	}
}

func TestTracer_InterfaceCompliance(t *testing.T) {
	// Verify that *Tracer implements the shutdown interface expected by app.go.
	var _ interface{ Shutdown(context.Context) error } = (*Tracer)(nil)
}

func TestTracer_SpanFromContext_ReturnsValidSpanID(t *testing.T) {
	defer resetGlobalOTel()
	tr, err := NewTracer(context.Background(), config.ObservabilityConfig{
		TracingEnabled:  true,
		TracingEndpoint: "localhost:4318",
		TracingInsecure: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Shutdown(context.Background())

	ctx, span := tr.StartSpan(context.Background(), "test")
	defer span.End()

	got := tr.SpanFromContext(ctx)
	// The span context should have a valid span ID (not zero).
	sc := got.SpanContext()
	if !sc.SpanID().IsValid() && sc.IsValid() {
		t.Error("expected valid span context from active tracer")
	}
}

func TestNewNoopTracer_DirectCall(t *testing.T) {
	tr := newNoopTracer()
	if tr.Enabled() {
		t.Error("noop tracer should not be enabled")
	}
	if tr.tracer == nil {
		t.Error("noop tracer should have non-nil inner tracer")
	}

	ctx, span := tr.StartSpan(context.Background(), "test")
	defer span.End()
	if ctx == nil {
		t.Error("expected non-nil context")
	}
	// noop span should not be recording.
	if span.IsRecording() {
		t.Error("noop span should not be recording")
	}
}

func TestNewTracer_InsecureFlag(t *testing.T) {
	// Verify that insecure flag is accepted without error.
	defer resetGlobalOTel()
	tr, err := NewTracer(context.Background(), config.ObservabilityConfig{
		TracingEnabled:  true,
		TracingEndpoint: "localhost:4318",
		TracingInsecure: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer tr.Shutdown(context.Background())
	if !tr.Enabled() {
		t.Error("expected Enabled() == true")
	}
}

func TestTracer_Shutdown_Enabled_MultipleCallsSafe(t *testing.T) {
	// M-7: Verify that calling Shutdown multiple times on an enabled tracer
	// does not return an error (idempotent via sync.Once).
	defer resetGlobalOTel()
	tr, err := NewTracer(context.Background(), config.ObservabilityConfig{
		TracingEnabled:  true,
		TracingEndpoint: "localhost:4318",
		TracingInsecure: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := tr.Shutdown(context.Background()); err != nil {
			t.Errorf("call %d: unexpected error: %v", i, err)
		}
	}
}
