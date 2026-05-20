package dm

import (
	"testing"

	"contractpro/legal-intelligence-core/internal/infra/observability/metrics"
)

// TestPublishOutcome_WireStringsPinned pins the local PublishOutcome mirror
// against the metrics.PublishOutcome SSOT (observability.md §3.9, the
// authoritative enum lives at metrics/labels.go:170-177) so the hermetic
// mirror cannot silently drift from the value the LIC-TASK-036 / TASK-047
// adapter feeds to lic_publisher_messages_total{outcome}.
//
// Identical guard pattern to base.TestOutcome_WireStringsPinned /
// router.TestCallOutcome_WireStringsPinned / cost / schemavalidator. This
// is the ONLY file in this package that imports
// internal/infra/observability/metrics, and it is a _test file, so package
// hermeticity (asserted by internal_test.go.TestHermeticImports over the
// non-test sources) holds.
func TestPublishOutcome_WireStringsPinned(t *testing.T) {
	pairs := []struct {
		local PublishOutcome
		ssot  metrics.PublishOutcome
	}{
		{PublishOutcomeSuccess, metrics.PublishOutcomeSuccess},
		{PublishOutcomeFailure, metrics.PublishOutcomeFailure},
		{PublishOutcomeNacked, metrics.PublishOutcomeNacked},
		{PublishOutcomeInvalid, metrics.PublishOutcomeInvalid},
	}
	for _, p := range pairs {
		if p.local.String() != string(p.ssot) {
			t.Fatalf("PublishOutcome %q drifted from metrics SSOT %q",
				p.local, p.ssot)
		}
		if !p.local.IsValid() {
			t.Fatalf("PublishOutcome %q not IsValid", p.local)
		}
	}
	if PublishOutcome("nope").IsValid() {
		t.Fatal("unknown outcome reported valid")
	}
}
