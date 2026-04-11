package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// newTestTracer returns a Tracer backed by an in-memory exporter for
// asserting on exported spans in tests.
func newTestTracer(t *testing.T) (*Tracer, *tracetest.InMemoryExporter) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	return &Tracer{
		tracer:       tp.Tracer(serviceName),
		provider:     tp,
		shutdownFunc: tp.Shutdown,
		enabled:      true,
	}, exporter
}

func TestHTTPMiddleware_NoOp_PassesThrough(t *testing.T) {
	tr := newNoopTracer()
	mw := HTTPMiddleware(tr)

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHTTPMiddleware_SpanInContext(t *testing.T) {
	tr, _ := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	var spanCtx trace.SpanContext
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spanCtx = trace.SpanFromContext(r.Context()).SpanContext()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !spanCtx.IsValid() {
		t.Error("expected valid span context in request context")
	}
}

func TestHTTPMiddleware_SetsAttributes(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name != SpanHTTPRequest {
		t.Errorf("expected span name %q, got %q", SpanHTTPRequest, span.Name)
	}

	// Verify http.method attribute.
	found := false
	for _, attr := range span.Attributes {
		if attr.Key == "http.method" && attr.Value.AsString() == "POST" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected http.method=POST attribute")
	}

	// Verify http.status_code attribute.
	found = false
	for _, attr := range span.Attributes {
		if attr.Key == "http.status_code" && attr.Value.AsInt64() == 201 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected http.status_code=201 attribute")
	}
}

func TestHTTPMiddleware_CorrelationIDAttribute(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Inject RequestContext with correlation_id into the request context.
	ctx := logger.WithRequestContext(req.Context(), logger.RequestContext{
		CorrelationID: "test-corr-123",
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == AttrCorrelationID && attr.Value.AsString() == "test-corr-123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected orch.correlation_id=test-corr-123 attribute")
	}
}

func TestHTTPMiddleware_AuthContextAttributes(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := auth.WithAuthContext(req.Context(), auth.AuthContext{
		OrganizationID: "org-456",
		UserID:         "user-789",
		Role:           "LAWYER",
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	orgFound, userFound := false, false
	for _, attr := range spans[0].Attributes {
		if attr.Key == AttrOrganizationID && attr.Value.AsString() == "org-456" {
			orgFound = true
		}
		if attr.Key == AttrUserID && attr.Value.AsString() == "user-789" {
			userFound = true
		}
	}
	if !orgFound {
		t.Error("expected orch.organization_id=org-456 attribute")
	}
	if !userFound {
		t.Error("expected orch.user_id=user-789 attribute")
	}
}

func TestHTTPMiddleware_ServerErrorSetsSpanError(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if spans[0].Status.Code != codes.Error {
		t.Errorf("expected span status Error, got %v", spans[0].Status.Code)
	}
}

func TestHTTPMiddleware_SuccessDoesNotSetSpanError(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if spans[0].Status.Code == codes.Error {
		t.Error("200 response should not set span error status")
	}
}

func TestHTTPMiddleware_DefaultStatusCode(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	// Handler writes body without explicit WriteHeader → implicit 200.
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "http.status_code" && attr.Value.AsInt64() == 200 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected http.status_code=200 for implicit status")
	}
}

func TestHTTPMiddleware_ChiRoutePattern(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	r := chi.NewRouter()
	r.Use(mw)
	r.Get("/api/v1/contracts/{contract_id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/abc-123", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "http.route" && attr.Value.AsString() == "/api/v1/contracts/{contract_id}" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected http.route=/api/v1/contracts/{contract_id} attribute (chi pattern)")
	}
}

func TestHTTPMiddleware_SpanKindServer(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if spans[0].SpanKind != trace.SpanKindServer {
		t.Errorf("expected SpanKindServer, got %v", spans[0].SpanKind)
	}
}

func TestHTTPMiddleware_PropagatesParentSpan(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a child span inside the handler.
		_, childSpan := tr.StartSpan(r.Context(), "child.span")
		childSpan.End()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	// Find child and parent spans.
	var childSpan, parentSpan tracetest.SpanStub
	for _, s := range spans {
		if s.Name == "child.span" {
			childSpan = s
		} else if s.Name == SpanHTTPRequest {
			parentSpan = s
		}
	}

	// The child span's parent should be the HTTP request span.
	if childSpan.Parent.SpanID() != parentSpan.SpanContext.SpanID() {
		t.Error("child span should have the HTTP request span as parent")
	}

	// Both should share the same trace ID.
	if childSpan.SpanContext.TraceID() != parentSpan.SpanContext.TraceID() {
		t.Error("child and parent spans should share the same trace ID")
	}
}

func TestHTTPMiddleware_4xxDoesNotSetSpanError(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if spans[0].Status.Code == codes.Error {
		t.Error("4xx response should not set span error status")
	}
}

func TestResponseCapture_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	rc := &responseCapture{ResponseWriter: rec, statusCode: http.StatusOK}

	// httptest.ResponseRecorder implements http.Flusher.
	rc.Flush()
	if !rec.Flushed {
		t.Error("expected Flush to delegate to underlying ResponseWriter")
	}
}

func TestResponseCapture_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	rc := &responseCapture{ResponseWriter: rec, statusCode: http.StatusOK}

	if rc.Unwrap() != rec {
		t.Error("Unwrap should return the underlying ResponseWriter")
	}
}

func TestResponseCapture_WriteHeaderIdempotent(t *testing.T) {
	rec := httptest.NewRecorder()
	rc := &responseCapture{ResponseWriter: rec, statusCode: http.StatusOK}

	rc.WriteHeader(http.StatusCreated)
	rc.WriteHeader(http.StatusInternalServerError) // should be ignored

	if rc.statusCode != http.StatusCreated {
		t.Errorf("expected first status code 201, got %d", rc.statusCode)
	}
}

func TestHTTPMiddleware_ConcurrentSafety(t *testing.T) {
	tr, _ := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestHTTPMiddleware_NoAuthContextDoesNotPanic(t *testing.T) {
	tr, exp := newTestTracer(t)
	defer tr.Shutdown(context.Background())
	mw := HTTPMiddleware(tr)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// No AuthContext injected — should not panic.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	// Verify no org/user attributes are set.
	for _, attr := range spans[0].Attributes {
		if attr.Key == AttrOrganizationID || attr.Key == AttrUserID {
			t.Error("should not set org/user attributes without AuthContext")
		}
	}
}

