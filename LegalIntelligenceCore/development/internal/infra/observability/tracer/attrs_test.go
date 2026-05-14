package tracer

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestSpanFields_AsKeyValues_OmitsEmpty(t *testing.T) {
	got := SpanFields{
		CorrelationID:  "cid-1",
		JobID:          "job-1",
		OrganizationID: "org-1",
	}.AsKeyValues()
	if len(got) != 3 {
		t.Fatalf("expected exactly 3 attrs, got %d", len(got))
	}
	keys := map[attribute.Key]bool{}
	for _, kv := range got {
		keys[kv.Key] = true
	}
	for _, k := range []attribute.Key{AttrCorrelationID, AttrJobID, AttrOrganizationID} {
		if !keys[k] {
			t.Fatalf("expected %s", k)
		}
	}
}

func TestSpanFields_AsKeyValues_AllFieldsCovered(t *testing.T) {
	full := SpanFields{
		CorrelationID:     "cid",
		JobID:             "job",
		VersionID:         "v",
		DocumentID:        "doc",
		OrganizationID:    "org",
		CreatedByUserID:   "u1",
		ConfirmedByUserID: "u2",
		MessageID:         "m",
		ParentVersionID:   "pv",
	}
	got := full.AsKeyValues()
	if len(got) != 9 {
		t.Fatalf("expected 9 attrs (one per field), got %d", len(got))
	}
}

func TestAttrConstants_StableNamespacing(t *testing.T) {
	// Lock the wire-format strings — dashboards and alerts depend on
	// these exact attribute keys (observability.md §4.3).
	cases := map[attribute.Key]string{
		AttrCorrelationID:                         "correlation_id",
		AttrJobID:                                 "job_id",
		AttrVersionID:                             "version_id",
		AttrDocumentID:                            "document_id",
		AttrOrganizationID:                        "organization_id",
		AttrCreatedByUserID:                       "created_by_user_id",
		AttrConfirmedByUserID:                     "confirmed_by_user_id",
		AttrMessageID:                             "message_id",
		AttrParentVersionID:                       "parent_version_id",
		AttrPipelineMode:                          "lic.pipeline.mode",
		AttrPipelineOriginType:                    "lic.pipeline.origin_type",
		AttrPipelineOutcome:                       "lic.pipeline.outcome",
		AttrPipelinePromptInjectionDetected:       "lic.pipeline.prompt_injection.detected",
		AttrPipelinePromptInjectionDetectionCount: "lic.pipeline.prompt_injection.detection_count",
		AttrPipelinePromptInjectionDetectedBy:     "lic.pipeline.prompt_injection.detected_by_agents",
		AttrPipelineStage:                         "lic.pipeline.stage",
		AttrAgentID:                               "lic.agent.id",
		AttrAgentOutcome:                          "lic.agent.outcome",
		AttrAgentRepairAttempts:                   "lic.agent.repair_attempts",
		AttrAgentPromptInjectionDetected:          "lic.agent.prompt_injection_detected",
		AttrLLMProvider:                           "lic.llm.provider",
		AttrLLMModel:                              "lic.llm.model",
		AttrLLMInputTokens:                        "lic.llm.input_tokens",
		AttrLLMCachedTokens:                       "lic.llm.cached_tokens",
		AttrLLMOutputTokens:                       "lic.llm.output_tokens",
		AttrLLMLatencyMS:                          "lic.llm.latency_ms",
		AttrLLMCostUSD:                            "lic.llm.cost_usd",
		AttrLLMFallbackUsed:                       "lic.llm.fallback_used",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Fatalf("attribute key drift: %q != %q (renaming breaks dashboards)", string(got), want)
		}
	}
}
