package model

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"

	"contractpro/api-orchestrator/internal/domain/model/validation"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

var validationCounter *prometheus.CounterVec

// SetValidationCounter registers the Prometheus counter used by WriteValidationError.
// Must be called once during application startup, before any HTTP requests are served.
func SetValidationCounter(c *prometheus.CounterVec) {
	validationCounter = c
}

// WriteValidationError writes a structured 400 VALIDATION_ERROR response
// with field-level details from the ValidationErrorBuilder.
//
// For each field error it:
//   - Logs the field path and code at DEBUG level (observability for top-N validation errors)
//   - Increments the orch_validation_errors_total Prometheus counter (if registered)
func WriteValidationError(w http.ResponseWriter, r *http.Request, verr *validation.ValidationError, log *logger.Logger) {
	ctx := r.Context()
	endpoint := resolveEndpoint(r)

	for _, f := range verr.Details.Fields {
		log.Debug(ctx, "validation field error",
			"field", f.Field,
			"code", string(f.Code),
		)
		if validationCounter != nil {
			validationCounter.WithLabelValues(endpoint, string(f.Code)).Inc()
		}
	}

	WriteError(w, r, ErrValidationError, verr.Details)
}

func resolveEndpoint(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return r.URL.Path
}
