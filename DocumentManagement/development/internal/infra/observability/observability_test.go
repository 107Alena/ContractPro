package observability

import (
	"context"
	"testing"

	"contractpro/document-management/internal/config"
)

func TestNew_DefaultConfig(t *testing.T) {
	cfg := config.ObservabilityConfig{
		LogLevel:        "info",
		MetricsPort:     9090,
		TracingEnabled:  false,
		TracingEndpoint: "",
	}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if sdk.Logger == nil {
		t.Fatal("Logger is nil")
	}
	if sdk.Metrics == nil {
		t.Fatal("Metrics is nil")
	}
	if sdk.Tracer == nil {
		t.Fatal("Tracer is nil")
	}
	if sdk.Tracer.Enabled() {
		t.Fatal("Tracer should be disabled with empty endpoint")
	}
}

func TestNew_ServiceFieldInLogger(t *testing.T) {
	cfg := config.ObservabilityConfig{LogLevel: "info"}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// The Logger should have "service" = "document-management" via With().
	// We verify it's a non-nil logger with the inner slog set.
	if sdk.Logger.Slog() == nil {
		t.Fatal("Logger.Slog() is nil")
	}
}

func TestSDK_Shutdown(t *testing.T) {
	cfg := config.ObservabilityConfig{
		LogLevel:       "info",
		TracingEnabled: false,
	}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := sdk.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
}

func TestNew_MetricsRegistryNotNil(t *testing.T) {
	cfg := config.ObservabilityConfig{LogLevel: "info"}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if sdk.Metrics.Registry() == nil {
		t.Fatal("Metrics.Registry() is nil")
	}
}

func TestNew_DebugLogLevel(t *testing.T) {
	cfg := config.ObservabilityConfig{LogLevel: "debug"}

	sdk, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if sdk.Logger == nil {
		t.Fatal("Logger is nil for debug level")
	}
}
