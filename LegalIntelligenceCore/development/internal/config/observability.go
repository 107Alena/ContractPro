package config

import "fmt"

// ObservabilityConfig holds OpenTelemetry and Prometheus settings.
type ObservabilityConfig struct {
	OTELEndpoint     string  // LIC_OTEL_EXPORTER_OTLP_ENDPOINT — gRPC endpoint (may be empty: tracing disabled)
	OTELInsecure     bool    // LIC_OTEL_EXPORTER_OTLP_INSECURE — TLS off (dev only)
	TracesSampler    string  // LIC_OTEL_TRACES_SAMPLER
	TracesSamplerArg float64 // LIC_OTEL_TRACES_SAMPLER_ARG — 0.0 ≤ x ≤ 1.0
	ServiceName      string  // LIC_OTEL_SERVICE_NAME
	MetricsPath      string  // LIC_METRICS_PATH — Prometheus scrape path
}

func loadObservabilityConfig() ObservabilityConfig {
	return ObservabilityConfig{
		OTELEndpoint:     envString("LIC_OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OTELInsecure:     envBool("LIC_OTEL_EXPORTER_OTLP_INSECURE", false),
		TracesSampler:    envString("LIC_OTEL_TRACES_SAMPLER", "parentbased_traceidratio"),
		TracesSamplerArg: envFloat64("LIC_OTEL_TRACES_SAMPLER_ARG", 0.1),
		ServiceName:      envString("LIC_OTEL_SERVICE_NAME", "lic-service"),
		MetricsPath:      envString("LIC_METRICS_PATH", "/metrics"),
	}
}

func (o ObservabilityConfig) validate() error {
	if o.TracesSamplerArg < 0 || o.TracesSamplerArg > 1 {
		return fmt.Errorf("config: LIC_OTEL_TRACES_SAMPLER_ARG must be in [0,1], got %v", o.TracesSamplerArg)
	}
	if o.MetricsPath == "" || o.MetricsPath[0] != '/' {
		return fmt.Errorf("config: LIC_METRICS_PATH must start with '/', got %q", o.MetricsPath)
	}
	if o.ServiceName == "" {
		return fmt.Errorf("config: LIC_OTEL_SERVICE_NAME must not be empty")
	}
	return nil
}
