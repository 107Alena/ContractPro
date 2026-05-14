package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNewDomainError_PopulatesFromCatalog(t *testing.T) {
	de := NewDomainError(ErrCodeAnalysisTimeout, StageAgentRiskDetection)
	if de.Code != ErrCodeAnalysisTimeout {
		t.Errorf("Code = %q, want %q", de.Code, ErrCodeAnalysisTimeout)
	}
	if !de.Retryable {
		t.Error("ANALYSIS_TIMEOUT must be retryable per error-handling.md §3.5")
	}
	if de.UserMessage == "" {
		t.Error("UserMessage must be populated from catalog (RU)")
	}
	if !strings.Contains(de.DevMessage, "job-level context deadline") {
		t.Errorf("DevMessage = %q, expected catalog default to mention context deadline", de.DevMessage)
	}
	if de.Stage != StageAgentRiskDetection {
		t.Errorf("Stage = %q, want %q", de.Stage, StageAgentRiskDetection)
	}
	if de.Cause != nil {
		t.Errorf("Cause = %v, want nil for fresh construction", de.Cause)
	}
	if de.Attributes != nil {
		t.Errorf("Attributes = %v, want nil for fresh construction", de.Attributes)
	}
}

func TestNewDomainError_DLQOnlyCodes_HaveEmptyUserMessage(t *testing.T) {
	for _, c := range []ErrorCode{
		ErrCodeInvalidMessageSchema,
		ErrCodeInvalidOrgIDMismatch,
		ErrCodeIdempotencyStoreUnavail,
	} {
		de := NewDomainError(c, StageReceived)
		if de.UserMessage != "" {
			t.Errorf("ErrorCode %q is DLQ-only — NewDomainError must leave UserMessage empty, got %q", c, de.UserMessage)
		}
		if c.IsPublishableToOrchestrator() {
			t.Errorf("ErrorCode %q must report IsPublishableToOrchestrator()=false", c)
		}
	}
}

func TestNewDomainError_PublishableCodes_HaveNonEmptyUserMessage(t *testing.T) {
	// Spot-check: codes published to Orchestrator carry a RU UserMessage AND
	// IsPublishableToOrchestrator() == true.
	for _, c := range []ErrorCode{
		ErrCodeAnalysisTimeout,
		ErrCodeDMArtifactsTimeout,
		ErrCodeUserConfirmationExpired,
		ErrCodeInvalidContractType,
	} {
		de := NewDomainError(c, StageReceived)
		if de.UserMessage == "" {
			t.Errorf("ErrorCode %q published to Orchestrator must have non-empty UserMessage", c)
		}
		if !c.IsPublishableToOrchestrator() {
			t.Errorf("ErrorCode %q must report IsPublishableToOrchestrator()=true", c)
		}
	}
}

func TestNewDomainError_PanicsOnUnknownCode(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewDomainError must panic on unknown ErrorCode")
		}
	}()
	_ = NewDomainError(ErrorCode("UNREGISTERED"), StageReceived)
}

func TestDomainError_WithCause(t *testing.T) {
	cause := errors.New("context deadline exceeded")
	de := NewDomainError(ErrCodeAnalysisTimeout, StageAgentRiskDetection).WithCause(cause)
	if !errors.Is(de, cause) {
		t.Error("errors.Is must traverse DomainError.Cause set by WithCause")
	}
	// Nil receiver passthrough.
	var nilDE *DomainError
	if got := nilDE.WithCause(cause); got != nil {
		t.Errorf("nilDE.WithCause = %v, want nil", got)
	}
}

func TestDomainError_WithDevMessage_Overrides(t *testing.T) {
	const custom = "elapsed_ms=90123 agent_id=AGENT_RISK_DETECTION"
	de := NewDomainError(ErrCodeAnalysisTimeout, StageAgentRiskDetection).WithDevMessage(custom)
	if de.DevMessage != custom {
		t.Errorf("DevMessage = %q, want override %q", de.DevMessage, custom)
	}
}

func TestDomainError_WithUserMessage_OverridesForVariantSpec(t *testing.T) {
	// DM_PERSIST_FAILED has a non-retryable RU variant per
	// error-handling.md §3.2 («Документ был удалён или недоступен.»).
	// Caller switches to it via WithRetryable(false) + WithUserMessage.
	const nonRetryableRU = "Документ был удалён или недоступен."
	de := NewDomainError(ErrCodeDMPersistFailed, StagePublishingArtifacts).
		WithRetryable(false).
		WithUserMessage(nonRetryableRU)
	if de.Retryable {
		t.Error("WithRetryable(false) must override the catalog default")
	}
	if de.UserMessage != nonRetryableRU {
		t.Errorf("UserMessage = %q, want %q", de.UserMessage, nonRetryableRU)
	}
}

func TestDomainError_WithRetryable(t *testing.T) {
	// AGENT_DEPENDENCY_FAILED default is false; flip via builder.
	de := NewDomainError(ErrCodeAgentDependencyFailed, StageAgentRecommendation).WithRetryable(true)
	if !de.Retryable {
		t.Error("WithRetryable(true) must flip the catalog default")
	}
}

func TestDomainError_WithAttributes_TransfersOwnership(t *testing.T) {
	attrs := map[string]any{"agent_id": "AGENT_RISK_DETECTION", "elapsed_ms": 90000}
	de := NewDomainError(ErrCodeAnalysisTimeout, StageAgentRiskDetection).WithAttributes(attrs)
	if v := de.Attributes["agent_id"]; v != "AGENT_RISK_DETECTION" {
		t.Errorf("Attributes[agent_id] = %v, want AGENT_RISK_DETECTION", v)
	}
	// Ownership transfer contract: caller mutation after the call is
	// reflected in the DomainError (documented; locks the behaviour).
	attrs["new_key"] = "new_value"
	if de.Attributes["new_key"] != "new_value" {
		t.Error("WithAttributes is documented as ownership-transfer; mutation should reflect")
	}
}

func TestDomainError_WithAttributes_NilClears(t *testing.T) {
	de := NewDomainError(ErrCodeInternal, StageReceived).
		WithAttribute("a", 1).
		WithAttributes(nil)
	if de.Attributes != nil {
		t.Errorf("Attributes = %v, want nil after WithAttributes(nil)", de.Attributes)
	}
}

func TestDomainError_WithAttribute_LazilyAllocates(t *testing.T) {
	de := NewDomainError(ErrCodeInternal, StageReceived).WithAttribute("k", "v")
	if de.Attributes["k"] != "v" {
		t.Errorf("WithAttribute on nil map failed: got %v", de.Attributes)
	}
}

func TestDomainError_WithAttribute_NilReceiverReturnsNil(t *testing.T) {
	var de *DomainError
	if got := de.WithAttribute("k", "v"); got != nil {
		t.Errorf("nil receiver WithAttribute returned %v, want nil", got)
	}
}

func TestDomainError_Builder_ChainsCorrectly(t *testing.T) {
	cause := errors.New("inner")
	de := NewDomainError(ErrCodeAgentTimeout, StageAgentRiskDetection).
		WithCause(cause).
		WithDevMessage("agent_id=AGENT_RISK_DETECTION timeout=12s").
		WithAttribute("agent_id", "AGENT_RISK_DETECTION").
		WithAttribute("timeout_s", 12)
	if de.Cause != cause || de.DevMessage == "" ||
		de.Attributes["agent_id"] != "AGENT_RISK_DETECTION" ||
		de.Attributes["timeout_s"] != 12 {
		t.Errorf("builder chain produced unexpected result: %+v", de)
	}
}

func TestDomainError_Error_FormatsWithStageAndCause(t *testing.T) {
	cause := errors.New("inner")
	de := NewDomainError(ErrCodeAnalysisTimeout, StageAgentRiskDetection).
		WithDevMessage("custom").
		WithCause(cause)
	s := de.Error()
	if !strings.Contains(s, string(ErrCodeAnalysisTimeout)) {
		t.Errorf("Error() %q missing code", s)
	}
	if !strings.Contains(s, string(StageAgentRiskDetection)) {
		t.Errorf("Error() %q missing stage", s)
	}
	if !strings.Contains(s, "custom") {
		t.Errorf("Error() %q missing dev message", s)
	}
	if !strings.Contains(s, "inner") {
		t.Errorf("Error() %q missing cause text", s)
	}
	if de.UserMessage != "" && strings.Contains(s, de.UserMessage) {
		t.Error("Error() must NOT include UserMessage (RU) — that string is for the API surface")
	}
}

func TestDomainError_Error_FormatsWithoutCause(t *testing.T) {
	de := NewDomainError(ErrCodeAnalysisTimeout, StageAgentRiskDetection).WithDevMessage("custom")
	s := de.Error()
	if strings.Contains(s, "<nil>") {
		t.Errorf("Error() %q must not render <nil> when Cause is nil", s)
	}
}

func TestDomainError_Error_HandlesUnsetStage(t *testing.T) {
	de := &DomainError{Code: ErrCodeInternal, DevMessage: "msg"}
	s := de.Error()
	if !strings.Contains(s, "STAGE_UNSPECIFIED") {
		t.Errorf("Error() with unset Stage = %q, expected STAGE_UNSPECIFIED placeholder", s)
	}
}

func TestDomainError_Error_HandlesNilReceiver(t *testing.T) {
	var de *DomainError
	if got := de.Error(); got != "<nil>" {
		t.Errorf("(*DomainError)(nil).Error() = %q, want <nil>", got)
	}
}

func TestDomainError_Unwrap(t *testing.T) {
	cause := errors.New("boom")
	de := NewDomainError(ErrCodeInternal, StageReceived).WithCause(cause)
	if got := errors.Unwrap(de); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}
	var nilDE *DomainError
	if got := nilDE.Unwrap(); got != nil {
		t.Errorf("nil receiver Unwrap() = %v, want nil", got)
	}
}

func TestDomainError_AsViaErrorsAs(t *testing.T) {
	de := NewDomainError(ErrCodeAgentTimeout, StageAgentRiskDetection)
	wrapped := fmt.Errorf("pipeline error: %w", de)
	var got *DomainError
	if !errors.As(wrapped, &got) {
		t.Fatal("errors.As must locate DomainError through fmt.Errorf wrap")
	}
	if got.Code != ErrCodeAgentTimeout {
		t.Errorf("As-extracted code = %q, want %q", got.Code, ErrCodeAgentTimeout)
	}
}

func TestIsDomainError(t *testing.T) {
	de := NewDomainError(ErrCodeAgentTimeout, StageAgentRiskDetection)
	if !IsDomainError(de) {
		t.Error("IsDomainError must return true for *DomainError")
	}
	wrapped := fmt.Errorf("wrap: %w", de)
	if !IsDomainError(wrapped) {
		t.Error("IsDomainError must traverse error chain")
	}
	if IsDomainError(errors.New("plain")) {
		t.Error("IsDomainError must return false for plain error")
	}
	if IsDomainError(nil) {
		t.Error("IsDomainError(nil) must return false")
	}
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		code      ErrorCode
		wantRetry bool
	}{
		{ErrCodeAnalysisTimeout, true},
		{ErrCodeDocumentTooLarge, false},
		{ErrCodeAgentInputTooLarge, false},
		{ErrCodeLLMAllProvidersFailed, true},
		{ErrCodeInvalidContractType, false},
	}
	for _, tc := range cases {
		de := NewDomainError(tc.code, StageReceived)
		if got := IsRetryable(de); got != tc.wantRetry {
			t.Errorf("IsRetryable(%v) = %v, want %v", tc.code, got, tc.wantRetry)
		}
	}
	if IsRetryable(errors.New("plain")) {
		t.Error("IsRetryable on plain error must return false")
	}
}

func TestGetErrorCode(t *testing.T) {
	de := NewDomainError(ErrCodeAgentTimeout, StageAgentRiskDetection)
	if got := GetErrorCode(de); got != ErrCodeAgentTimeout {
		t.Errorf("GetErrorCode = %q, want %q", got, ErrCodeAgentTimeout)
	}
	if got := GetErrorCode(fmt.Errorf("wrap: %w", de)); got != ErrCodeAgentTimeout {
		t.Errorf("GetErrorCode through wrap = %q, want %q", got, ErrCodeAgentTimeout)
	}
	if got := GetErrorCode(errors.New("plain")); got != "" {
		t.Errorf("GetErrorCode on plain error = %q, want empty", got)
	}
}

func TestAsDomainError(t *testing.T) {
	de := NewDomainError(ErrCodeAgentTimeout, StageAgentRiskDetection)
	if got, ok := AsDomainError(de); !ok || got.Code != ErrCodeAgentTimeout {
		t.Errorf("AsDomainError direct = (%v, %v), want (Code=%q, true)", got, ok, ErrCodeAgentTimeout)
	}
	wrapped := fmt.Errorf("pipeline: %w", de)
	if got, ok := AsDomainError(wrapped); !ok || got.Code != ErrCodeAgentTimeout {
		t.Errorf("AsDomainError through wrap failed: got=%v ok=%v", got, ok)
	}
	if got, ok := AsDomainError(errors.New("plain")); ok || got != nil {
		t.Errorf("AsDomainError on plain error = (%v, %v), want (nil, false)", got, ok)
	}
}
