package metrics

import "testing"

// TestLabels_BoolLabel — central truth for bool-to-label conversion.
func TestLabels_BoolLabel(t *testing.T) {
	if got := BoolLabel(true); got != "true" {
		t.Errorf("BoolLabel(true) = %q, want %q", got, "true")
	}
	if got := BoolLabel(false); got != "false" {
		t.Errorf("BoolLabel(false) = %q, want %q", got, "false")
	}
}

// TestLabels_OutcomeValues — locks the exact enum strings against
// observability.md §3 spec text. Changes here are coordinated with
// dashboards and alert rules — a silent rename breaks production.
func TestLabels_OutcomeValues(t *testing.T) {
	cases := map[string]string{
		"PipelineModeInitial":            string(PipelineModeInitial),
		"PipelineModeRecheck":            string(PipelineModeRecheck),
		"PipelineOutcomeSuccess":         string(PipelineOutcomeSuccess),
		"PipelineOutcomeFailed":          string(PipelineOutcomeFailed),
		"PipelineOutcomeTimeout":         string(PipelineOutcomeTimeout),
		"AgentOutcomeSuccess":            string(AgentOutcomeSuccess),
		"AgentOutcomeRepairSuccess":      string(AgentOutcomeRepairSuccess),
		"AgentOutcomeInvalidOutput":      string(AgentOutcomeInvalidOutput),
		"AgentOutcomeProviderError":      string(AgentOutcomeProviderError),
		"AgentOutcomeTimeout":            string(AgentOutcomeTimeout),
		"RepairOutcomeRepairedOK":        string(RepairOutcomeRepairedOK),
		"RepairOutcomeRepairFailed":      string(RepairOutcomeRepairFailed),
		"RepairOutcomeProviderError":     string(RepairOutcomeProviderError),
		"LLMOutcomeSuccess":              string(LLMOutcomeSuccess),
		"LLMOutcomeRepair":               string(LLMOutcomeRepair),
		"LLMOutcomeFail":                 string(LLMOutcomeFail),
		"LLMOutcomeFallback":             string(LLMOutcomeFallback),
		"LLMErrTimeout":                  string(LLMErrTimeout),
		"LLMErrInvalidAPIKey":            string(LLMErrInvalidAPIKey),
		"LLMHealthHealthy":               string(LLMHealthHealthy),
		"LLMHealthUnhealthy":             string(LLMHealthUnhealthy),
		"LLMHealthPermanent":             string(LLMHealthPermanent),
		"DMOpGetArtifacts":               string(DMOpGetArtifacts),
		"DMOpPersistArtifacts":           string(DMOpPersistArtifacts),
		"DMOutcomeSuccess":               string(DMOutcomeSuccess),
		"DMOutcomeTimeout":               string(DMOutcomeTimeout),
		"DMOutcomePersistFailed":         string(DMOutcomePersistFailed),
		"DMOutcomeMissing":               string(DMOutcomeMissing),
		"IdempLookupNew":                 string(IdempLookupNew),
		"IdempLookupInProgress":          string(IdempLookupInProgress),
		"IdempLookupCompleted":           string(IdempLookupCompleted),
		"IdempLookupFallbackDB":          string(IdempLookupFallbackDB),
		"PendingOutcomeResumed":          string(PendingOutcomeResumed),
		"PendingOutcomeExpired":          string(PendingOutcomeExpired),
		"PendingOutcomeInvalid":          string(PendingOutcomeInvalid),
		"DLQTopicInvalidMessage":         string(DLQTopicInvalidMessage),
		"DLQTopicAgentOutputInvalid":     string(DLQTopicAgentOutputInvalid),
		"PartyValidationINN":             string(PartyValidationINN),
		"PartyValidationOGRN":            string(PartyValidationOGRN),
	}
	expected := map[string]string{
		"PipelineModeInitial":            "INITIAL",
		"PipelineModeRecheck":            "RE_CHECK",
		"PipelineOutcomeSuccess":         "success",
		"PipelineOutcomeFailed":          "failed",
		"PipelineOutcomeTimeout":         "timeout",
		"AgentOutcomeSuccess":            "success",
		"AgentOutcomeRepairSuccess":      "repair_success",
		"AgentOutcomeInvalidOutput":      "invalid_output",
		"AgentOutcomeProviderError":      "provider_error",
		"AgentOutcomeTimeout":            "timeout",
		"RepairOutcomeRepairedOK":        "repaired_ok",
		"RepairOutcomeRepairFailed":      "repair_failed",
		"RepairOutcomeProviderError":     "repair_provider_error",
		"LLMOutcomeSuccess":              "success",
		"LLMOutcomeRepair":               "repair",
		"LLMOutcomeFail":                 "fail",
		"LLMOutcomeFallback":             "fallback",
		"LLMErrTimeout":                  "TIMEOUT",
		"LLMErrInvalidAPIKey":            "INVALID_API_KEY",
		"LLMHealthHealthy":               "healthy",
		"LLMHealthUnhealthy":             "unhealthy",
		"LLMHealthPermanent":             "permanent",
		"DMOpGetArtifacts":               "get_artifacts",
		"DMOpPersistArtifacts":           "persist_artifacts",
		"DMOutcomeSuccess":               "success",
		"DMOutcomeTimeout":               "timeout",
		"DMOutcomePersistFailed":         "persist_failed",
		"DMOutcomeMissing":               "missing",
		"IdempLookupNew":                 "new",
		"IdempLookupInProgress":          "in_progress",
		"IdempLookupCompleted":           "completed",
		"IdempLookupFallbackDB":          "fallback_db",
		"PendingOutcomeResumed":          "resumed",
		"PendingOutcomeExpired":          "expired",
		"PendingOutcomeInvalid":          "invalid",
		"DLQTopicInvalidMessage":         "lic.dlq.invalid-message",
		"DLQTopicAgentOutputInvalid":     "lic.dlq.agent-output-invalid",
		"PartyValidationINN":             "inn",
		"PartyValidationOGRN":            "ogrn",
	}

	for name, got := range cases {
		want, ok := expected[name]
		if !ok {
			t.Errorf("test inconsistency: missing expected for %s", name)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestLabels_CircuitStateNumeric — the gauge encoding is part of the
// dashboard contract: 0 closed → 1 half_open → 2 open.
func TestLabels_CircuitStateNumeric(t *testing.T) {
	if CircuitStateClosed != 0 {
		t.Errorf("CircuitStateClosed = %v, want 0", CircuitStateClosed)
	}
	if CircuitStateHalfOpen != 1 {
		t.Errorf("CircuitStateHalfOpen = %v, want 1", CircuitStateHalfOpen)
	}
	if CircuitStateOpen != 2 {
		t.Errorf("CircuitStateOpen = %v, want 2", CircuitStateOpen)
	}
}
