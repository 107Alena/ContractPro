package tracer

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
)

// InjectIntoHeaders writes the W3C trace context (traceparent,
// tracestate, baggage) from ctx into headers. The map is mutated in
// place; pre-existing entries with the same keys are overwritten.
//
// LIC publishers feed RabbitMQ amqp.Table → map[string]string into
// this helper at the egress boundary.
//
// Passing a nil headers map is a programmer error — silently dropping
// traceparent on egress would break the cross-domain propagation
// invariant (observability.md §4.4) and leave no audit trail.
func (t *Tracer) InjectIntoHeaders(ctx context.Context, headers map[string]string) {
	if headers == nil {
		panic("tracer: InjectIntoHeaders requires a non-nil headers map (would silently drop traceparent on egress)")
	}
	t.propagator.Inject(ctx, propagation.MapCarrier(headers))
}

// ExtractFromHeaders returns a context enriched with any W3C trace
// context found in headers. Missing headers leave ctx unchanged.
//
// LIC consumers convert RabbitMQ amqp.Table → map[string]string at
// the ingress boundary and feed the result here.
func (t *Tracer) ExtractFromHeaders(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	return t.propagator.Extract(ctx, propagation.MapCarrier(headers))
}
