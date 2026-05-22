package dlq

import (
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics"
)

// TestPublishOutcome_WireStringsPinned pins the local PublishOutcome mirror
// against the metrics.PublishOutcome SSOT (observability.md §3.9, the
// authoritative enum lives at metrics/labels.go:170-177) so the hermetic
// mirror cannot silently drift from the value the LIC-TASK-047 adapter
// feeds to lic_publisher_messages_total{outcome}.
//
// Identical guard pattern to base.TestOutcome_WireStringsPinned /
// router.TestCallOutcome_WireStringsPinned / cost / schemavalidator / the
// sibling dm + orch publishers. This is the ONLY file in this package that
// imports internal/infra/observability/metrics, and it is a _test file, so
// package hermeticity (asserted by internal_test.go.TestHermeticImports
// over the non-test sources) holds.
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

// TestDLQReason_TopicMappingExhaustive pins the actual topicToReason()
// behaviour, not just the existence of the reason constants. The previous
// version of this test never called topicToReason and was a misleading
// drift detector — fixed per code-reviewer M1.
//
// Adding a new DLQTopic constant without updating topicToReason would
// silently route the new topic through the "unknown" default arm; this
// test fails loudly if that happens (the new topic's case absence here
// means port.DLQTopic.IsValid() rejects it earlier, but if a future edit
// allows it through Block A and into the switch, the "unknown" string
// would land in lic_dlq_published_total{reason} and balloon cardinality
// past the §3.10 budget).
func TestDLQReason_TopicMappingExhaustive(t *testing.T) {
	cases := []struct {
		topic  port.DLQTopic
		reason string
	}{
		{port.DLQTopicInvalidMessage, reasonInvalidMessage},
		{port.DLQTopicConsumerFailed, reasonConsumerFailed},
		{port.DLQTopicPublishFailed, reasonPublishFailed},
		{port.DLQTopicAgentOutputInvalid, reasonAgentOutputInvalid},
	}

	// Each of the four declared topics MUST map to its expected reason
	// constant — explicit per-case assertion against the actual function.
	seen := make(map[string]struct{}, len(cases))
	for _, c := range cases {
		got := topicToReason(c.topic)
		if got != c.reason {
			t.Errorf("topicToReason(%q) = %q, want %q", c.topic, got, c.reason)
		}
		if got == "unknown" {
			t.Errorf("topicToReason(%q) hit the defensive \"unknown\" default arm — Block A allowed an unknown topic through", c.topic)
		}
		seen[c.reason] = struct{}{}
	}
	if len(seen) != len(cases) {
		t.Fatalf("topic→reason mapping has duplicates: %d unique reasons for %d topics", len(seen), len(cases))
	}
}

// TestTopicToReason_DefensiveDefault exercises the unreachable-in-production
// "unknown" default arm (code-reviewer M2). PublishDLQ's Block A rejects
// invalid topics before topicToReason is reached, so the defensive default
// is dead code today — but a future refactor that reorders validation or
// adds a new DLQTopic constant without updating the switch would route
// silently through this arm. The test pins the behaviour so the regression
// is visible.
func TestTopicToReason_DefensiveDefault(t *testing.T) {
	got := topicToReason(port.DLQTopic("lic.dlq.brand-new-topic"))
	if got != "unknown" {
		t.Errorf("topicToReason(unknown) = %q, want %q", got, "unknown")
	}
}
