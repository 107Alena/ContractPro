package observability

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// globalOTelOnce ensures the global OTel propagator and provider are
// set exactly once, even if NewTracer is called multiple times.
var globalOTelOnce sync.Once

// Tracer wraps an OpenTelemetry TracerProvider and exposes a small,
// application-friendly API. When tracing is disabled or no endpoint is
// configured, it falls back to a no-op provider so callers never need
// nil checks.
type Tracer struct {
	tracer   trace.Tracer
	shutdown func(ctx context.Context) error
	enabled  bool
}

// NewTracer creates an OpenTelemetry tracer for the given service name.
//
// If enabled is false or endpoint is empty, a no-op tracer is returned.
// Otherwise an OTLP/HTTP exporter is configured with a BatchSpanProcessor,
// and the global W3C TextMapPropagator (TraceContext + Baggage) is set.
// When insecure is true, the exporter uses plaintext HTTP; otherwise TLS.
func NewTracer(ctx context.Context, serviceName string, enabled bool, endpoint string, insecure bool) (*Tracer, error) {
	if !enabled || endpoint == "" {
		noopProvider := noop.NewTracerProvider()
		return &Tracer{
			tracer:   noopProvider.Tracer(serviceName),
			shutdown: func(context.Context) error { return nil },
			enabled:  false,
		}, nil
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Set global OTel provider and propagator exactly once so that
	// third-party libraries using otel.Tracer() participate in traces
	// and outgoing HTTP requests carry W3C trace context headers.
	globalOTelOnce.Do(func() {
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
	})

	return &Tracer{
		tracer:   tp.Tracer(serviceName),
		shutdown: tp.Shutdown,
		enabled:  true,
	}, nil
}

// StartSpan starts a new span with the given name.
// Returns the enriched context and the span. Callers must call span.End().
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, name, opts...)
}

// SpanFromContext returns the current span from the context.
func (t *Tracer) SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// Shutdown flushes pending spans and releases exporter resources.
// Safe to call on a no-op tracer (returns nil immediately).
func (t *Tracer) Shutdown(ctx context.Context) error {
	return t.shutdown(ctx)
}

// Enabled reports whether the tracer is actively exporting spans.
func (t *Tracer) Enabled() bool {
	return t.enabled
}
