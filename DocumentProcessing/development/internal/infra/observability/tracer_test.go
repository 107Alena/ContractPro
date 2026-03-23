package observability

import (
	"context"
	"testing"
)

func TestNewTracer_disabledReturnNoOp(t *testing.T) {
	t.Parallel()

	tr, err := NewTracer(context.Background(), "dp-worker", false, "http://jaeger:4318", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr == nil {
		t.Fatal("NewTracer returned nil")
	}
	if tr.Enabled() {
		t.Error("expected Enabled() == false for disabled tracer")
	}
}

func TestNewTracer_emptyEndpointReturnsNoOp(t *testing.T) {
	t.Parallel()

	tr, err := NewTracer(context.Background(), "dp-worker", true, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr == nil {
		t.Fatal("NewTracer returned nil")
	}
	if tr.Enabled() {
		t.Error("expected Enabled() == false for empty endpoint")
	}
}

func TestNewTracer_disabledAndEmptyEndpoint(t *testing.T) {
	t.Parallel()

	tr, err := NewTracer(context.Background(), "dp-worker", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Enabled() {
		t.Error("expected Enabled() == false")
	}
}

func TestTracer_StartSpan_noOp_noPanic(t *testing.T) {
	t.Parallel()

	tr, err := NewTracer(context.Background(), "dp-worker", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, span := tr.StartSpan(context.Background(), "test-span")
	if ctx == nil {
		t.Error("StartSpan returned nil context")
	}
	if span == nil {
		t.Error("StartSpan returned nil span")
	}
	// End should not panic on no-op span.
	span.End()
}

func TestTracer_SpanFromContext_returnsSpan(t *testing.T) {
	t.Parallel()

	tr, err := NewTracer(context.Background(), "dp-worker", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, span := tr.StartSpan(context.Background(), "parent-span")
	defer span.End()

	got := tr.SpanFromContext(ctx)
	if got == nil {
		t.Error("SpanFromContext returned nil")
	}
}

func TestTracer_SpanFromContext_backgroundContext(t *testing.T) {
	t.Parallel()

	tr, err := NewTracer(context.Background(), "dp-worker", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SpanFromContext on a bare background context should still return
	// a valid (no-op) span, not nil.
	got := tr.SpanFromContext(context.Background())
	if got == nil {
		t.Error("SpanFromContext returned nil for background context")
	}
}

func TestTracer_Shutdown_noOp_noError(t *testing.T) {
	t.Parallel()

	tr, err := NewTracer(context.Background(), "dp-worker", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := tr.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown on no-op tracer returned error: %v", err)
	}
}

func TestTracer_Shutdown_canBeCalledMultipleTimes(t *testing.T) {
	t.Parallel()

	tr, err := NewTracer(context.Background(), "dp-worker", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := tr.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown call %d returned error: %v", i+1, err)
		}
	}
}

func TestTracer_Enabled_reflectsState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		endpoint string
		want     bool
	}{
		{"disabled with endpoint", false, "http://jaeger:4318", false},
		{"enabled with empty endpoint", true, "", false},
		{"disabled with empty endpoint", false, "", false},
		// Note: enabled=true + valid endpoint would try to connect,
		// so we skip that case in unit tests.
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr, err := NewTracer(context.Background(), "dp-worker", tc.enabled, tc.endpoint, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := tr.Enabled(); got != tc.want {
				t.Errorf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
