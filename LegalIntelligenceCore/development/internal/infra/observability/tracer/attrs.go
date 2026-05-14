// Package tracer provides OpenTelemetry tracing for LIC service.
//
// This file declares the typed attribute keys for all spans. Treat the
// list as the single source of truth for span schema (observability.md
// §4.3). Any new key must be added here so call sites cannot drift.
package tracer

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Correlation keys present on every span (observability.md §4.3).
//
// Wire-format strings are STABLE — dashboards, alerts, and Tempo/Jaeger
// queries pin against them. Renaming any key here is a breaking change.
const (
	AttrCorrelationID     = attribute.Key("correlation_id")
	AttrJobID             = attribute.Key("job_id")
	AttrVersionID         = attribute.Key("version_id")
	AttrDocumentID        = attribute.Key("document_id")
	AttrOrganizationID    = attribute.Key("organization_id")
	AttrCreatedByUserID   = attribute.Key("created_by_user_id")
	AttrConfirmedByUserID = attribute.Key("confirmed_by_user_id")
	AttrMessageID         = attribute.Key("message_id")
	AttrParentVersionID   = attribute.Key("parent_version_id")
)

// Pipeline-level keys (root span).
const (
	AttrPipelineMode                          = attribute.Key("lic.pipeline.mode")
	AttrPipelineOriginType                    = attribute.Key("lic.pipeline.origin_type")
	AttrPipelineOutcome                       = attribute.Key("lic.pipeline.outcome")
	AttrPipelinePromptInjectionDetected       = attribute.Key("lic.pipeline.prompt_injection.detected")
	AttrPipelinePromptInjectionDetectionCount = attribute.Key("lic.pipeline.prompt_injection.detection_count")
	AttrPipelinePromptInjectionDetectedBy     = attribute.Key("lic.pipeline.prompt_injection.detected_by_agents")
	AttrPipelineStage                         = attribute.Key("lic.pipeline.stage")
)

// Agent span keys.
const (
	AttrAgentID                      = attribute.Key("lic.agent.id")
	AttrAgentOutcome                 = attribute.Key("lic.agent.outcome")
	AttrAgentRepairAttempts          = attribute.Key("lic.agent.repair_attempts")
	AttrAgentPromptInjectionDetected = attribute.Key("lic.agent.prompt_injection_detected")
)

// LLM span keys.
const (
	AttrLLMProvider     = attribute.Key("lic.llm.provider")
	AttrLLMModel        = attribute.Key("lic.llm.model")
	AttrLLMInputTokens  = attribute.Key("lic.llm.input_tokens")
	AttrLLMCachedTokens = attribute.Key("lic.llm.cached_tokens")
	AttrLLMOutputTokens = attribute.Key("lic.llm.output_tokens")
	AttrLLMLatencyMS    = attribute.Key("lic.llm.latency_ms")
	AttrLLMCostUSD      = attribute.Key("lic.llm.cost_usd")
	AttrLLMFallbackUsed = attribute.Key("lic.llm.fallback_used")
)

// SpanFields groups the correlation-ID set that almost every span
// carries. It is a *plain* container — apply it once near span start
// to pay a single SetAttributes call (one allocation).
type SpanFields struct {
	CorrelationID     string
	JobID             string
	VersionID         string
	DocumentID        string
	OrganizationID    string
	CreatedByUserID   string
	ConfirmedByUserID string
	MessageID         string
	ParentVersionID   string
}

// AsKeyValues returns the non-empty fields as an attribute slice.
// Empty strings are skipped — span schemas in observability.md §4.3
// document only the IDs that exist for the given event class.
//
// Prefer ApplyTo when the goal is to attach fields to an existing
// span; AsKeyValues is for callers who need to merge with extra
// attributes before SetAttributes.
func (f SpanFields) AsKeyValues() []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, 9)
	if f.CorrelationID != "" {
		out = append(out, AttrCorrelationID.String(f.CorrelationID))
	}
	if f.JobID != "" {
		out = append(out, AttrJobID.String(f.JobID))
	}
	if f.VersionID != "" {
		out = append(out, AttrVersionID.String(f.VersionID))
	}
	if f.DocumentID != "" {
		out = append(out, AttrDocumentID.String(f.DocumentID))
	}
	if f.OrganizationID != "" {
		out = append(out, AttrOrganizationID.String(f.OrganizationID))
	}
	if f.CreatedByUserID != "" {
		out = append(out, AttrCreatedByUserID.String(f.CreatedByUserID))
	}
	if f.ConfirmedByUserID != "" {
		out = append(out, AttrConfirmedByUserID.String(f.ConfirmedByUserID))
	}
	if f.MessageID != "" {
		out = append(out, AttrMessageID.String(f.MessageID))
	}
	if f.ParentVersionID != "" {
		out = append(out, AttrParentVersionID.String(f.ParentVersionID))
	}
	return out
}

// ApplyTo attaches the populated fields to span via a single
// SetAttributes call. No-op for nil / non-recording spans.
func (f SpanFields) ApplyTo(span trace.Span) {
	if span == nil || !span.IsRecording() {
		return
	}
	if kvs := f.AsKeyValues(); len(kvs) > 0 {
		span.SetAttributes(kvs...)
	}
}
