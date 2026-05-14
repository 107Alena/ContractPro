package tracer

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// StartSpan starts a span with the supplied attributes attached
// in-line (one SetAttributes batch internally — single allocation).
// Caller must call returned span.End().
func (t *Tracer) StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if len(attrs) == 0 {
		return t.tracer.Start(ctx, name)
	}
	return t.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// StartSpanWithFields starts a span and applies the SpanFields
// correlation IDs as a single batched attribute update. This is the
// preferred entry point for pipeline / agent / LLM call sites.
func (t *Tracer) StartSpanWithFields(ctx context.Context, name string, fields SpanFields, extra ...attribute.KeyValue) (context.Context, trace.Span) {
	kvs := fields.AsKeyValues()
	if len(extra) > 0 {
		kvs = append(kvs, extra...)
	}
	return t.StartSpan(ctx, name, kvs...)
}

// SpanFromContext is a thin re-export so call sites do not need a
// direct dependency on go.opentelemetry.io/otel/trace.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// RecordError marks span as errored, records the error event, and
// (when err != nil) sets status=Error with the error message. No-op
// for nil error; no-op for non-recording spans.
func RecordError(span trace.Span, err error) {
	if err == nil || span == nil || !span.IsRecording() {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetOK is a convenience setter for the success path. Equivalent to
// span.SetStatus(codes.Ok, "") with a non-recording-span guard.
func SetOK(span trace.Span) {
	if span == nil || !span.IsRecording() {
		return
	}
	span.SetStatus(codes.Ok, "")
}
