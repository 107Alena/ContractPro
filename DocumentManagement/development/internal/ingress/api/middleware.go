package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// metricsMiddleware records dm_api_requests_total and dm_api_request_duration_seconds.
func metricsMiddleware(requests *prometheus.CounterVec, duration *prometheus.HistogramVec) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rw, r)

			elapsed := time.Since(start).Seconds()
			// Use the URL pattern as "path" label for cardinality control.
			// r.Pattern is available in Go 1.22+ with method-aware routing.
			path := r.URL.Path
			if r.Pattern != "" {
				path = r.Pattern
			}

			statusStr := strconv.Itoa(rw.statusCode)
			requests.WithLabelValues(r.Method, path, statusStr).Inc()
			duration.WithLabelValues(r.Method, path).Observe(elapsed)
		})
	}
}

// loggingMiddleware logs each HTTP request with method, path, status and duration.
// When metrics middleware is active, it reuses the existing responseWriter to
// avoid double wrapping. Otherwise, it creates its own.
func loggingMiddleware(logger Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Reuse existing responseWriter if already wrapped by metrics middleware.
			rw, ok := w.(*responseWriter)
			if !ok {
				rw = &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			}

			next.ServeHTTP(rw, r)

			elapsed := time.Since(start)
			logger.Info("api request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.statusCode,
				"duration_ms", elapsed.Milliseconds(),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
// Only one instance is created per request (shared between middleware).
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
	}
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher if the underlying ResponseWriter supports it.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for use with
// http.ResponseController (Go 1.20+).
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}
