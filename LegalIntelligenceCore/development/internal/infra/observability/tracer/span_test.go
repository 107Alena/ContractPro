package tracer

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newRecordingTracer builds a Tracer whose spans are captured by an
// in-memory exporter via a SimpleSpanProcessor (synchronous). Use it
// when the test does not need to exercise batching/flush ordering.
// InstallGlobals=false so the process-wide otel.SetTracerProvider is
// not perturbed.
func newRecordingTracer(t *testing.T) (*Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return &Tracer{
		tracer: tp.Tracer("lic-test"),
		tp:     tp,
		propagator: propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{}, propagation.Baggage{},
		),
		enabled: true,
	}, exp
}

func TestStartSpan_RecordsNameAndAttributes(t *testing.T) {
	tr, exp := newRecordingTracer(t)
	ctx, span := tr.StartSpan(context.Background(), "lic.pipeline",
		AttrPipelineMode.String("INITIAL"),
	)
	if !span.IsRecording() {
		t.Fatal("span must be recording")
	}
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	got := spans[0]
	if got.Name != "lic.pipeline" {
		t.Fatalf("span name = %q, want lic.pipeline", got.Name)
	}
	if attrValue(got.Attributes, AttrPipelineMode) != "INITIAL" {
		t.Fatalf("expected lic.pipeline.mode=INITIAL, got %q", attrValue(got.Attributes, AttrPipelineMode))
	}
	_ = ctx
}

func TestStartSpanWithFields_AppliesAllNonEmpty(t *testing.T) {
	tr, exp := newRecordingTracer(t)
	fields := SpanFields{
		CorrelationID:  "cid-1",
		JobID:          "job-1",
		VersionID:      "v-1",
		OrganizationID: "org-1",
	}
	_, span := tr.StartSpanWithFields(context.Background(), "lic.agent.type_classifier", fields,
		AttrAgentID.String("AGENT_TYPE_CLASSIFIER"),
	)
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	a := spans[0].Attributes
	if attrValue(a, AttrCorrelationID) != "cid-1" {
		t.Fatal("correlation_id missing")
	}
	if attrValue(a, AttrJobID) != "job-1" {
		t.Fatal("job_id missing")
	}
	if attrValue(a, AttrAgentID) != "AGENT_TYPE_CLASSIFIER" {
		t.Fatal("lic.agent.id missing — extra attrs must be merged after fields")
	}
	// Unset SpanFields entries must NOT produce empty-string attributes.
	if hasKey(a, AttrDocumentID) {
		t.Fatal("empty SpanFields entries must be omitted, but document_id was set")
	}
}

func TestRecordError_SetsErrorStatus(t *testing.T) {
	tr, exp := newRecordingTracer(t)
	_, span := tr.StartSpan(context.Background(), "lic.dm.persist.await")
	RecordError(span, errors.New("dm timeout"))
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Fatalf("expected status.code=Error, got %v", spans[0].Status.Code)
	}
	if spans[0].Status.Description != "dm timeout" {
		t.Fatalf("expected status.description=dm timeout, got %q", spans[0].Status.Description)
	}
	if len(spans[0].Events) == 0 {
		t.Fatal("RecordError must add an exception event")
	}
}

func TestRecordError_NilGuards(t *testing.T) {
	// nil error is a no-op
	tr, exp := newRecordingTracer(t)
	_, span := tr.StartSpan(context.Background(), "lic.dm.publish.analysis_ready")
	RecordError(span, nil)
	span.End()
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Status.Code == codes.Error {
		t.Fatal("nil error must not promote span status to Error")
	}

	// nil span must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RecordError(nil span) panicked: %v", r)
		}
	}()
	RecordError(nil, errors.New("any"))
}

func TestSetOK_MarksSpanOK(t *testing.T) {
	tr, exp := newRecordingTracer(t)
	_, span := tr.StartSpan(context.Background(), "lic.calc.aggregate_score")
	SetOK(span)
	span.End()
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Status.Code != codes.Ok {
		t.Fatalf("expected status.code=Ok, got %v", spans[0].Status.Code)
	}
}

func TestSpanFromContext_RecoversCurrentSpan(t *testing.T) {
	tr, _ := newRecordingTracer(t)
	ctx, span := tr.StartSpan(context.Background(), "root")
	defer span.End()
	got := SpanFromContext(ctx)
	if got.SpanContext().SpanID() != span.SpanContext().SpanID() {
		t.Fatal("SpanFromContext must return the active span")
	}
}

// helpers

func attrValue(kvs []attribute.KeyValue, key attribute.Key) string {
	for _, kv := range kvs {
		if kv.Key == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

func hasKey(kvs []attribute.KeyValue, key attribute.Key) bool {
	for _, kv := range kvs {
		if kv.Key == key {
			return true
		}
	}
	return false
}
