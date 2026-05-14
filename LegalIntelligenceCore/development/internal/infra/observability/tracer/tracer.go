package tracer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Sampler names accepted in LIC_OTEL_TRACES_SAMPLER (subset of the
// OTel SDK env spec). Anything else is rejected at validation time.
const (
	SamplerAlwaysOn                  = "always_on"
	SamplerAlwaysOff                 = "always_off"
	SamplerTraceIDRatio              = "traceidratio"
	SamplerParentBasedAlwaysOn       = "parentbased_always_on"
	SamplerParentBasedAlwaysOff      = "parentbased_always_off"
	SamplerParentBasedTraceIDRatio   = "parentbased_traceidratio"
)

// shutdownTimeout caps total Shutdown wall-clock; flushTimeout caps
// ForceFlush alone so a slow exporter cannot starve the underlying
// connection close. Total upper bound = flushTimeout + shutdownTimeout.
const (
	flushTimeout    = 3 * time.Second
	shutdownTimeout = 5 * time.Second
)

// installGlobalsOnce guards a process-wide install of the global OTel
// TracerProvider and TextMapPropagator. The install is intentionally
// one-shot: once any New(InstallGlobals=true) wins, later New() calls
// silently skip the install (their *Tracer is still returned and
// usable through DI). The global pointer is therefore frozen for the
// lifetime of the process — required for auto-instrumentation libs
// (otelgrpc, otelhttp) that read otel.GetTracerProvider().
var installGlobalsOnce sync.Once

// Config carries the validated subset of LIC observability config the
// tracer needs.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Endpoint       string  // empty → no-op tracer
	Insecure       bool    // gRPC plaintext (dev only)
	Sampler        string  // see Sampler* constants; empty == ParentBased(TraceIDRatio)
	SamplerArg     float64 // 0.0..1.0 (used by ratio samplers)
	InstallGlobals bool    // true in production wiring; false in tests
}

// Tracer wraps an OpenTelemetry TracerProvider and exposes the few
// helpers LIC call sites need. When Config.Endpoint is empty the
// tracer is a no-op (still propagator-aware, so upstream traceparent
// is preserved).
type Tracer struct {
	tracer     trace.Tracer
	tp         *sdktrace.TracerProvider // nil for no-op
	propagator propagation.TextMapPropagator
	enabled    bool
}

// New builds the tracer. Returns a no-op (Enabled=false) tracer when
// Endpoint is empty. Otherwise it constructs an OTLP gRPC exporter, a
// BatchSpanProcessor, the W3C composite propagator, and optionally
// installs them as the global OTel state.
func New(ctx context.Context, cfg Config) (*Tracer, error) {
	if cfg.ServiceName == "" {
		return nil, errors.New("tracer: ServiceName is required")
	}
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	if cfg.Endpoint == "" {
		t := &Tracer{
			tracer:     noop.NewTracerProvider().Tracer(cfg.ServiceName),
			propagator: propagator,
			enabled:    false,
		}
		if cfg.InstallGlobals {
			t.installGlobals()
		}
		return t, nil
	}

	sampler, err := buildSampler(cfg.Sampler, cfg.SamplerArg)
	if err != nil {
		return nil, err
	}

	exporter, err := newOTLPExporter(ctx, cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, fmt.Errorf("tracer: build OTLP exporter: %w", err)
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		// Best-effort cleanup so the half-built exporter does not leak.
		// Use a fresh background ctx in case the caller's ctx is already
		// cancelled — we still want gRPC connections to close.
		shCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		_ = exporter.Shutdown(shCtx)
		cancel()
		return nil, fmt.Errorf("tracer: build resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	t := &Tracer{
		tracer:     tp.Tracer(cfg.ServiceName),
		tp:         tp,
		propagator: propagator,
		enabled:    true,
	}
	if cfg.InstallGlobals {
		t.installGlobals()
	}
	return t, nil
}

// Tracer returns the underlying OTel tracer (escape hatch for code
// that needs trace.SpanStartOption directly).
func (t *Tracer) Tracer() trace.Tracer { return t.tracer }

// Propagator returns the configured composite propagator.
func (t *Tracer) Propagator() propagation.TextMapPropagator { return t.propagator }

// Enabled reports whether the tracer actively exports spans.
func (t *Tracer) Enabled() bool { return t.enabled }

// Shutdown flushes batched spans and releases exporter resources.
// ForceFlush and Shutdown each get an independent bounded ctx so a
// flush that consumes its full budget cannot starve the gRPC close.
//
// Calling Shutdown on a no-op tracer is safe and returns nil.
func (t *Tracer) Shutdown(ctx context.Context) error {
	if t.tp == nil {
		return nil
	}
	flushCtx, cancelFlush := context.WithTimeout(ctx, flushTimeout)
	defer cancelFlush()
	flushErr := t.tp.ForceFlush(flushCtx)

	shutCtx, cancelShut := context.WithTimeout(ctx, shutdownTimeout)
	defer cancelShut()
	shutErr := t.tp.Shutdown(shutCtx)

	return errors.Join(flushErr, shutErr)
}

// installGlobals wires t into otel.GetTracerProvider() and
// otel.GetTextMapPropagator() exactly once (process-wide). Tests
// should pass InstallGlobals=false and use the returned Tracer
// directly.
func (t *Tracer) installGlobals() {
	installGlobalsOnce.Do(func() {
		if t.tp != nil {
			otel.SetTracerProvider(t.tp)
		}
		otel.SetTextMapPropagator(t.propagator)
	})
}

// buildSampler validates the requested sampler name + arg and returns
// an sdktrace.Sampler. Unknown names are rejected — silent fallback
// would hide a typo in env config.
func buildSampler(name string, arg float64) (sdktrace.Sampler, error) {
	switch name {
	case "", SamplerParentBasedTraceIDRatio:
		if arg < 0 || arg > 1 {
			return nil, fmt.Errorf("tracer: SamplerArg must be in [0,1] for %q, got %v", SamplerParentBasedTraceIDRatio, arg)
		}
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(arg)), nil
	case SamplerTraceIDRatio:
		if arg < 0 || arg > 1 {
			return nil, fmt.Errorf("tracer: SamplerArg must be in [0,1] for %q, got %v", SamplerTraceIDRatio, arg)
		}
		return sdktrace.TraceIDRatioBased(arg), nil
	case SamplerAlwaysOn:
		return sdktrace.AlwaysSample(), nil
	case SamplerAlwaysOff:
		return sdktrace.NeverSample(), nil
	case SamplerParentBasedAlwaysOn:
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case SamplerParentBasedAlwaysOff:
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	default:
		return nil, fmt.Errorf("tracer: unknown sampler %q (supported: always_on, always_off, traceidratio, parentbased_always_on, parentbased_always_off, parentbased_traceidratio)", name)
	}
}

// newOTLPExporter is split out so tests can stub the network call.
func newOTLPExporter(ctx context.Context, endpoint string, insecure bool) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(ctx, opts...)
}

// buildResource seeds attributes that downstream dashboards rely on.
// resource.Default() pulls process.runtime.* / host.* via the SDK's
// built-in detectors, so we do not duplicate them by hand. We do
// *not* set a schema URL on the custom resource: resource.Merge
// rejects two non-empty conflicting URLs, and the SDK's default
// resource already advertises the SDK-bundled semconv URL.
func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceInstanceID(instanceID()),
		semconv.ProcessRuntimeName("go"),
		semconv.ProcessRuntimeVersion(runtime.Version()),
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironment(cfg.Environment))
	}

	customRes, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, err
	}
	return resource.Merge(resource.Default(), customRes)
}

// instanceID returns a 16-byte hex string unique per process boot. We
// avoid pulling a uuid dependency: 128 bits from crypto/rand has
// adequate uniqueness for the per-pod identity dimension and matches
// what semconv ServiceInstanceID expects (opaque string).
//
// crypto/rand failure means a broken OS entropy source — fail loudly
// rather than seed every replica with the same "unknown" string and
// silently break Tempo/Jaeger per-instance aggregation.
func instanceID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("tracer: crypto/rand failed (broken entropy source): %v", err))
	}
	return hex.EncodeToString(buf[:])
}
