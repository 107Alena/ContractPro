package tracing

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

// InjectHTTPHeaders injects W3C traceparent and tracestate headers into
// the outgoing HTTP request for trace propagation across sync HTTP calls
// (e.g., Orchestrator → DM).
//
// When tracing is disabled, the global propagator is a no-op and this
// function adds no headers (zero overhead).
//
// Note: X-Correlation-Id is already handled by dmclient.setHeaders and
// does not need to be injected here.
func InjectHTTPHeaders(ctx context.Context, req *http.Request) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
}

// ExtractHTTPHeaders extracts W3C traceparent and tracestate from an
// incoming HTTP request, returning a context enriched with the remote
// span context. Used to continue a trace from an upstream service.
//
// When tracing is disabled, the global propagator is a no-op and this
// returns the original context unchanged.
func ExtractHTTPHeaders(ctx context.Context, req *http.Request) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(req.Header))
}

// CorrelationAttribute returns a span attribute for the correlation_id.
// Used to link async event processing spans to the originating request
// trace via the correlation_id field in the event envelope.
func CorrelationAttribute(correlationID string) attribute.KeyValue {
	return AttrCorrelationID.String(correlationID)
}
