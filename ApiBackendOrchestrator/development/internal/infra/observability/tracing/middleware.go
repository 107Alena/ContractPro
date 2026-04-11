package tracing

import (
	"net/http"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// responseCapture wraps http.ResponseWriter to capture the status code
// for span attributes. It delegates all writes to the underlying writer.
type responseCapture struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rc *responseCapture) WriteHeader(code int) {
	if !rc.written {
		rc.statusCode = code
		rc.written = true
	}
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	if !rc.written {
		rc.statusCode = http.StatusOK
		rc.written = true
	}
	return rc.ResponseWriter.Write(b)
}

// Flush delegates to the underlying ResponseWriter if it supports
// http.Flusher (required for SSE streaming).
func (rc *responseCapture) Flush() {
	if f, ok := rc.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (rc *responseCapture) Unwrap() http.ResponseWriter {
	return rc.ResponseWriter
}

// HTTPMiddleware returns a chi-compatible middleware that creates the
// orch.http.request parent span for each incoming HTTP request.
//
// Span attributes set after the handler completes:
//   - http.method, http.route (chi route pattern), http.status_code
//   - orch.correlation_id, orch.organization_id, orch.user_id
//
// When the Tracer is disabled, noop spans add zero overhead.
//
// Placement in the middleware chain: AFTER SecurityHeadersMiddleware
// (which sets correlation_id) so the span can read the correlation_id
// from the request context.
func HTTPMiddleware(t *Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := t.StartSpan(r.Context(), SpanHTTPRequest,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
				),
			)
			defer span.End()

			rc := &responseCapture{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rc, r.WithContext(ctx))

			// Enrich the span with attributes available after handler completion.
			var attrs []attribute.KeyValue

			// Chi route pattern (not raw URL — prevents cardinality explosion).
			if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
				attrs = append(attrs, attribute.String("http.route", rctx.RoutePattern()))
			}
			attrs = append(attrs, attribute.Int("http.status_code", rc.statusCode))

			// Orchestrator context attributes from middleware.
			if reqCtx := logger.RequestContextFrom(ctx); reqCtx.CorrelationID != "" {
				attrs = append(attrs, AttrCorrelationID.String(reqCtx.CorrelationID))
			}
			if authCtx, ok := auth.AuthContextFrom(ctx); ok {
				attrs = append(attrs, AttrOrganizationID.String(authCtx.OrganizationID))
				attrs = append(attrs, AttrUserID.String(authCtx.UserID))
			}

			span.SetAttributes(attrs...)

			// Mark span as error for server error responses.
			if rc.statusCode >= 500 {
				span.SetStatus(codes.Error, http.StatusText(rc.statusCode))
			}
		})
	}
}
