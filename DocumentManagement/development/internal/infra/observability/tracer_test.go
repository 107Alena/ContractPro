package observability

import (
	"context"
	"testing"
)

func TestNewTracer_Disabled(t *testing.T) {
	tr, err := NewTracer(context.Background(), "test-svc", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Enabled() {
		t.Fatal("tracer should not be enabled")
	}
}

func TestNewTracer_DisabledWithEndpoint(t *testing.T) {
	tr, err := NewTracer(context.Background(), "test-svc", false, "localhost:4318", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Enabled() {
		t.Fatal("tracer should not be enabled when disabled=false")
	}
}

func TestNewTracer_EnabledEmptyEndpoint(t *testing.T) {
	tr, err := NewTracer(context.Background(), "test-svc", true, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Enabled() {
		t.Fatal("tracer should not be enabled with empty endpoint")
	}
}

func TestTracer_StartSpan_NoOp(t *testing.T) {
	tr, err := NewTracer(context.Background(), "test-svc", false, "", false)
	if err != nil {
		t.Fatal(err)
	}

	ctx, span := tr.StartSpan(context.Background(), "test-span")
	defer span.End()

	if ctx == nil {
		t.Fatal("context should not be nil")
	}
	if span == nil {
		t.Fatal("span should not be nil")
	}
	if span.IsRecording() {
		t.Fatal("no-op span should not be recording")
	}
}

func TestTracer_SpanFromContext_NoOp(t *testing.T) {
	tr, err := NewTracer(context.Background(), "test-svc", false, "", false)
	if err != nil {
		t.Fatal(err)
	}

	span := tr.SpanFromContext(context.Background())
	if span == nil {
		t.Fatal("span should not be nil")
	}
}

func TestTracer_Shutdown_NoOp(t *testing.T) {
	tr, err := NewTracer(context.Background(), "test-svc", false, "", false)
	if err != nil {
		t.Fatal(err)
	}

	if err := tr.Shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown should not error: %v", err)
	}
}

func TestTracer_Shutdown_MultipleCallsSafe(t *testing.T) {
	tr, err := NewTracer(context.Background(), "test-svc", false, "", false)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if err := tr.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown call %d should not error: %v", i, err)
		}
	}
}

func TestTracer_Enabled_ReflectsState(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		endpoint string
		want     bool
	}{
		{"disabled_no_endpoint", false, "", false},
		{"disabled_with_endpoint", false, "localhost:4318", false},
		{"enabled_no_endpoint", true, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := NewTracer(context.Background(), "test-svc", tt.enabled, tt.endpoint, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := tr.Enabled(); got != tt.want {
				t.Fatalf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
