package schemavalidator_test

import (
	"testing"

	"contractpro/legal-intelligence-core/internal/agents/schemavalidator"
)

// TestRepairOutcome_WireStringsPinned guards the local mirror against drift
// from the metrics.AgentRepairOutcome SSOT (metrics/labels.go:43-45 /
// observability.md §3.3 / error-handling.md §5.5). Importing the metrics
// package here would break this package's telemetry hermeticity, so the
// strings are pinned by literal — exactly like cost_test.go.
func TestRepairOutcome_WireStringsPinned(t *testing.T) {
	for o, want := range map[schemavalidator.RepairOutcome]string{
		schemavalidator.OutcomeRepairedOK:    "repaired_ok",
		schemavalidator.OutcomeRepairFailed:  "repair_failed",
		schemavalidator.OutcomeProviderError: "repair_provider_error",
	} {
		if o.String() != want {
			t.Fatalf("RepairOutcome %q != %q (SSOT drift vs metrics.AgentRepairOutcome)", o.String(), want)
		}
		if !o.IsValid() {
			t.Fatalf("%q.IsValid() = false, want true", o.String())
		}
	}
	if schemavalidator.RepairOutcome("bogus").IsValid() {
		t.Fatal(`RepairOutcome("bogus").IsValid() = true, want false`)
	}
}

func TestSchemaViolation_PrettyAndError(t *testing.T) {
	v := schemavalidator.NewValidator()
	err := v.Validate([]byte(objSchema), []byte(`{"ok":"x","extra":1}`))
	viol, ok := schemavalidator.AsSchemaViolation(err)
	if !ok {
		t.Fatalf("want *SchemaViolation, got %T", err)
	}
	if viol.Error() == "" || viol.Pretty() == "" {
		t.Fatal("SchemaViolation Error()/Pretty() must be non-empty")
	}
	// As-helpers must reject the other type.
	if _, ok := schemavalidator.AsSchemaCompileError(err); ok {
		t.Fatal("AsSchemaCompileError must be false for a SchemaViolation")
	}
}

func TestAsHelpers_NilSafe(t *testing.T) {
	if _, ok := schemavalidator.AsSchemaViolation(nil); ok {
		t.Fatal("AsSchemaViolation(nil) = true")
	}
	if _, ok := schemavalidator.AsSchemaCompileError(nil); ok {
		t.Fatal("AsSchemaCompileError(nil) = true")
	}
}
