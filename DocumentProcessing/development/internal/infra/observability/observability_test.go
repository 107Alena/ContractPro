package observability

import (
	"context"
	"testing"

	"contractpro/document-processing/internal/config"
)

func TestNew_defaultConfig_createsSDKWithAllComponentsNonNil(t *testing.T) {
	t.Parallel()

	cfg := config.ObservabilityConfig{
		LogLevel:        "info",
		MetricsPort:     9090,
		TracingEnabled:  false,
		TracingEndpoint: "",
	}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sdk == nil {
		t.Fatal("New returned nil SDK")
	}
	if sdk.Logger == nil {
		t.Error("SDK.Logger is nil")
	}
	if sdk.Metrics == nil {
		t.Error("SDK.Metrics is nil")
	}
	if sdk.Tracer == nil {
		t.Error("SDK.Tracer is nil")
	}
}

func TestNew_tracingDisabled_tracerNotEnabled(t *testing.T) {
	t.Parallel()

	cfg := config.ObservabilityConfig{
		LogLevel:        "debug",
		MetricsPort:     9090,
		TracingEnabled:  false,
		TracingEndpoint: "",
	}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sdk.Tracer.Enabled() {
		t.Error("expected Tracer.Enabled() == false when tracing is disabled")
	}
}

func TestNew_tracingEnabledButEmptyEndpoint_tracerNotEnabled(t *testing.T) {
	t.Parallel()

	cfg := config.ObservabilityConfig{
		LogLevel:        "info",
		MetricsPort:     9090,
		TracingEnabled:  true,
		TracingEndpoint: "",
	}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sdk.Tracer.Enabled() {
		t.Error("expected Tracer.Enabled() == false when endpoint is empty")
	}
}

func TestSDK_Shutdown_noError(t *testing.T) {
	t.Parallel()

	cfg := config.ObservabilityConfig{
		LogLevel:        "info",
		MetricsPort:     9090,
		TracingEnabled:  false,
		TracingEndpoint: "",
	}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := sdk.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}
}

func TestSDK_Shutdown_canBeCalledMultipleTimes(t *testing.T) {
	t.Parallel()

	cfg := config.ObservabilityConfig{
		LogLevel:        "warn",
		MetricsPort:     9090,
		TracingEnabled:  false,
		TracingEndpoint: "",
	}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := sdk.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown call %d returned error: %v", i+1, err)
		}
	}
}

func TestNew_withDifferentLogLevels(t *testing.T) {
	t.Parallel()

	levels := []string{"debug", "info", "warn", "error", "unknown"}

	for _, level := range levels {
		t.Run("level="+level, func(t *testing.T) {
			t.Parallel()

			cfg := config.ObservabilityConfig{
				LogLevel:       level,
				MetricsPort:    9090,
				TracingEnabled: false,
			}

			sdk, err := New(context.Background(), cfg)
			if err != nil {
				t.Fatalf("unexpected error for level %q: %v", level, err)
			}
			if sdk.Logger == nil {
				t.Errorf("SDK.Logger is nil for level %q", level)
			}
		})
	}
}

func TestNew_metricsRegistryIsAccessible(t *testing.T) {
	t.Parallel()

	cfg := config.ObservabilityConfig{
		LogLevel:       "info",
		MetricsPort:    9090,
		TracingEnabled: false,
	}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reg := sdk.Metrics.Registry()
	if reg == nil {
		t.Error("Metrics.Registry() returned nil")
	}
}
