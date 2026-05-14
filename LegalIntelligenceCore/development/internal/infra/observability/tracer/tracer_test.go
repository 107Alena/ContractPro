package tracer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// capturingExporter is a minimal SpanExporter that retains exported
// spans across Shutdown — needed because tracetest.InMemoryExporter
// resets its buffer in Shutdown, which would erase the very spans we
// want to verify after Tracer.Shutdown returns.
type capturingExporter struct {
	mu    sync.Mutex
	spans []tracetest.SpanStub
}

func (c *capturingExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spans = append(c.spans, tracetest.SpanStubsFromReadOnlySpans(spans)...)
	return nil
}

func (c *capturingExporter) Shutdown(context.Context) error { return nil }

func (c *capturingExporter) Get() []tracetest.SpanStub {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]tracetest.SpanStub, len(c.spans))
	copy(out, c.spans)
	return out
}

func TestNew_RequiresServiceName(t *testing.T) {
	_, err := New(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error when ServiceName is empty")
	}
	if !strings.Contains(err.Error(), "ServiceName") {
		t.Fatalf("error should mention ServiceName, got: %v", err)
	}
}

func TestNew_NoOpWhenEndpointEmpty(t *testing.T) {
	tr, err := New(context.Background(), Config{ServiceName: "lic-service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Enabled() {
		t.Fatal("expected disabled tracer when Endpoint is empty")
	}
	if tr.Tracer() == nil {
		t.Fatal("Tracer() should never return nil")
	}
	if tr.Propagator() == nil {
		t.Fatal("Propagator() should never return nil — needed to forward upstream traceparent")
	}
	// Shutdown of a no-op tracer must succeed and never panic.
	if err := tr.Shutdown(context.Background()); err != nil {
		t.Fatalf("no-op Shutdown returned error: %v", err)
	}
}

func TestBuildSampler(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		arg       float64
		wantError string
	}{
		{"default empty falls back to parentbased_traceidratio", "", 0.1, ""},
		{"parentbased_traceidratio with valid arg", SamplerParentBasedTraceIDRatio, 0.5, ""},
		{"traceidratio with valid arg", SamplerTraceIDRatio, 1.0, ""},
		{"always_on", SamplerAlwaysOn, 0, ""},
		{"always_off", SamplerAlwaysOff, 0, ""},
		{"parentbased_always_on", SamplerParentBasedAlwaysOn, 0, ""},
		{"parentbased_always_off", SamplerParentBasedAlwaysOff, 0, ""},
		{"unknown sampler rejected", "rando_sampler", 0, "unknown sampler"},
		{"negative arg rejected for parentbased_traceidratio", SamplerParentBasedTraceIDRatio, -0.1, "in [0,1]"},
		{"arg > 1 rejected for traceidratio", SamplerTraceIDRatio, 1.5, "in [0,1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := buildSampler(tt.input, tt.arg)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s == nil {
				t.Fatal("expected non-nil sampler")
			}
		})
	}
}

func TestBuildResource_AttributesPresent(t *testing.T) {
	res, err := buildResource(context.Background(), Config{
		ServiceName:    "lic-service",
		ServiceVersion: "1.2.3",
		Environment:    "production",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := map[attribute.Key]string{}
	for _, kv := range res.Attributes() {
		if kv.Value.Type() == attribute.STRING {
			got[kv.Key] = kv.Value.AsString()
		}
	}
	mustHave := map[attribute.Key]string{
		semconv.ServiceNameKey:           "lic-service",
		semconv.ServiceVersionKey:        "1.2.3",
		semconv.DeploymentEnvironmentKey: "production",
		semconv.ProcessRuntimeNameKey:    "go",
	}
	for k, want := range mustHave {
		if got[k] != want {
			t.Fatalf("attribute %s = %q, want %q", k, got[k], want)
		}
	}
	if got[semconv.ServiceInstanceIDKey] == "" || got[semconv.ServiceInstanceIDKey] == "unknown" {
		t.Fatalf("service.instance.id missing or 'unknown', got %q", got[semconv.ServiceInstanceIDKey])
	}
	if got[semconv.ProcessRuntimeVersionKey] == "" {
		t.Fatal("process.runtime.version must be populated from runtime.Version()")
	}
}

func TestBuildResource_OptionalAttributesOmitted(t *testing.T) {
	// Without ServiceVersion / Environment those attributes must NOT appear.
	res, err := buildResource(context.Background(), Config{ServiceName: "lic-service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, kv := range res.Attributes() {
		if kv.Key == semconv.ServiceVersionKey {
			t.Fatalf("service.version should be absent, got %q", kv.Value.AsString())
		}
		if kv.Key == semconv.DeploymentEnvironmentKey {
			t.Fatalf("deployment.environment should be absent, got %q", kv.Value.AsString())
		}
	}
}

func TestInstanceID_UniquePerCall(t *testing.T) {
	a := instanceID()
	b := instanceID()
	if a == b {
		t.Fatalf("instanceID returned same value twice: %q", a)
	}
	if len(a) != 32 {
		t.Fatalf("instanceID length = %d, want 32 hex chars (16 bytes)", len(a))
	}
}

func TestShutdown_BoundedTimeout(t *testing.T) {
	// No-op tracer Shutdown must accept any context — even a cancelled one.
	tr, err := New(context.Background(), Config{ServiceName: "lic-service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tr.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown of no-op tracer must tolerate cancelled context, got %v", err)
	}
}

func TestErrorJoinShutdownReturnsAnyFlushError(t *testing.T) {
	// Sanity check: errors.Join semantics we depend on. Ensures the
	// tracer Shutdown contract (flush_err, shutdown_err) → joined err
	// is observable.
	a := errors.New("flush failed")
	b := errors.New("shutdown failed")
	joined := errors.Join(a, b)
	if !errors.Is(joined, a) || !errors.Is(joined, b) {
		t.Fatal("errors.Join must surface both errors via errors.Is")
	}
}

func TestShutdown_FlushesPendingSpans(t *testing.T) {
	// SDK-backed Tracer with a real BatchSpanProcessor. Shutdown must
	// drain the batched span before returning. Use a capturing
	// exporter (vs InMemoryExporter, which wipes on Shutdown) so the
	// post-Shutdown assertion sees what was actually exported.
	exp := &capturingExporter{}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	tr := &Tracer{
		tracer:     tp.Tracer("lic-test"),
		tp:         tp,
		propagator: propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
		enabled:    true,
	}

	_, span := tr.StartSpan(context.Background(), "lic.pipeline")
	span.SetAttributes(AttrJobID.String("job-flush-1"))
	span.End()

	// Right after End() the span sits in the BatchSpanProcessor queue.
	// Without a flush it would only land on the next batch tick.
	if err := tr.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	spans := exp.Get()
	if len(spans) != 1 {
		t.Fatalf("Shutdown must flush queued spans before returning; got %d spans", len(spans))
	}
	if attrValue(spans[0].Attributes, AttrJobID) != "job-flush-1" {
		t.Fatalf("flushed span attributes lost")
	}
}

func TestSpanFields_ApplyTo_WritesNonEmptyFields(t *testing.T) {
	tr, exp := newRecordingTracer(t)
	_, span := tr.StartSpan(context.Background(), "lic.dm.artifacts.request")
	SpanFields{CorrelationID: "cid-X", JobID: "job-X"}.ApplyTo(span)
	span.End()
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	a := spans[0].Attributes
	if attrValue(a, AttrCorrelationID) != "cid-X" || attrValue(a, AttrJobID) != "job-X" {
		t.Fatal("ApplyTo must write populated fields onto span")
	}
}

func TestSpanFields_ApplyTo_NilGuards(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ApplyTo(nil span) panicked: %v", r)
		}
	}()
	SpanFields{CorrelationID: "x"}.ApplyTo(nil)
}
