package tracing

import sdktrace "go.opentelemetry.io/otel/sdk/trace"

// NewOrchSampler returns the sampling strategy for the orchestrator.
//
// Head-based sampling in the SDK:
//   - Root spans: 10% probability sampling (TraceIDRatioBased)
//   - Child spans: follow the parent's sampling decision
//
// True tail-based sampling (100% for errors, 100% for slow requests >2s)
// cannot be implemented in the SDK because the sampling decision is made
// at span creation time, before the outcome is known. The collector
// (Jaeger/Tempo) must be configured with tail-based sampling rules to
// additionally capture error and slow traces. See observability.md §3
// for the recommended collector configuration.
func NewOrchSampler() sdktrace.Sampler {
	return sdktrace.ParentBased(
		sdktrace.TraceIDRatioBased(0.10),
	)
}
