package tracing

import (
	"context"
	"fmt"
	"sync"

	"contractpro/api-orchestrator/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const serviceName = "api-orchestrator"

// Tracer wraps an OpenTelemetry TracerProvider and provides a simplified
// API for creating spans. When tracing is disabled, all methods are
// zero-cost no-ops backed by the OTel noop package.
type Tracer struct {
	tracer       trace.Tracer
	provider     trace.TracerProvider
	shutdownFunc func(ctx context.Context) error
	shutdownOnce sync.Once
	shutdownErr  error
	enabled      bool
}

// Compile-time check: *Tracer satisfies the Shutdown interface used by app.go.
var _ interface{ Shutdown(context.Context) error } = (*Tracer)(nil)

// NewTracer creates a Tracer from the ObservabilityConfig.
//
// When cfg.TracingEnabled is false or cfg.TracingEndpoint is empty, a no-op
// tracer is returned. The noop tracer has zero allocations per span —
// the Go compiler inlines the empty methods.
//
// When enabled, it configures:
//   - OTLP/HTTP exporter pointed at cfg.TracingEndpoint
//   - BatchSpanProcessor for async span export
//   - Custom sampler (ParentBased, 10% root sampling)
//   - W3C TraceContext + Baggage propagation (global)
//   - Resource with service.name = "api-orchestrator"
func NewTracer(ctx context.Context, cfg config.ObservabilityConfig) (*Tracer, error) {
	if !cfg.TracingEnabled || cfg.TracingEndpoint == "" {
		return newNoopTracer(), nil
	}

	// Configure OTLP/HTTP exporter.
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.TracingEndpoint),
	}
	if cfg.TracingInsecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(NewOrchSampler()),
		sdktrace.WithResource(res),
	)

	// Set global provider and propagator so third-party libraries
	// and InjectHTTPHeaders can participate in trace propagation.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Tracer{
		tracer:       tp.Tracer(serviceName),
		provider:     tp,
		shutdownFunc: tp.Shutdown,
		enabled:      true,
	}, nil
}

// newNoopTracer returns a Tracer backed by the OTel noop package.
// All operations are zero-cost. The global provider and propagator are
// NOT set, keeping the default no-op behaviour.
func newNoopTracer() *Tracer {
	np := noop.NewTracerProvider()
	return &Tracer{
		tracer:       np.Tracer(serviceName),
		provider:     np,
		shutdownFunc: func(context.Context) error { return nil },
		enabled:      false,
	}
}

// StartSpan starts a new span with the given name. Returns the enriched
// context and the span. Callers MUST call span.End() when done.
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, name, opts...)
}

// SpanFromContext returns the current span from the context.
// Returns a valid no-op span if no span is present (never nil).
func (t *Tracer) SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// Shutdown flushes pending spans and releases exporter resources.
// Safe to call multiple times (idempotent via sync.Once).
// No-op when tracing is disabled.
func (t *Tracer) Shutdown(ctx context.Context) error {
	t.shutdownOnce.Do(func() {
		t.shutdownErr = t.shutdownFunc(ctx)
	})
	return t.shutdownErr
}

// Enabled reports whether the tracer is actively exporting spans.
func (t *Tracer) Enabled() bool {
	return t.enabled
}

// Provider returns the underlying TracerProvider. This is useful for
// passing to third-party instrumentation libraries.
func (t *Tracer) Provider() trace.TracerProvider {
	return t.provider
}
