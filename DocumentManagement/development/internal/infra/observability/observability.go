package observability

import (
	"context"

	"contractpro/document-management/internal/config"
)

// SDK is the composite observability entry point for the Document Management
// service. It owns the Logger, Metrics, and Tracer and provides a single
// Shutdown method for graceful teardown.
//
// Create with New(); inject the returned *SDK into application-layer
// components via constructor parameters (no global singletons).
type SDK struct {
	Logger  *Logger
	Metrics *Metrics
	Tracer  *Tracer
}

// New initialises all observability subsystems from the given config.
// The returned SDK is ready to use immediately. Call Shutdown on service
// termination to flush pending traces.
func New(ctx context.Context, cfg config.ObservabilityConfig) (*SDK, error) {
	logger := NewLogger(cfg.LogLevel)
	logger = logger.With("service", "document-management")

	metrics := NewMetrics()

	tracer, err := NewTracer(ctx, "dm-service", cfg.TracingEnabled, cfg.TracingEndpoint, cfg.TracingInsecure)
	if err != nil {
		return nil, err
	}

	return &SDK{
		Logger:  logger,
		Metrics: metrics,
		Tracer:  tracer,
	}, nil
}

// Shutdown performs a graceful shutdown of all observability subsystems.
// Currently this flushes pending trace spans; Logger and Metrics have no
// shutdown requirements.
func (s *SDK) Shutdown(ctx context.Context) error {
	return s.Tracer.Shutdown(ctx)
}
