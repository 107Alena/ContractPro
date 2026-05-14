package model

import (
	"strings"
	"testing"
	"unicode"
)

func TestErrorCatalog_AllConstantsRegistered(t *testing.T) {
	// init() panics on mismatch at package load — this test reasserts the
	// invariant against the public API surface (AllErrorCodes ↔ catalog).
	for _, c := range AllErrorCodes() {
		if _, ok := errorCatalog[c]; !ok {
			t.Errorf("ErrorCode %q declared in AllErrorCodes but absent from errorCatalog", c)
		}
	}
	for c := range errorCatalog {
		var found bool
		for _, decl := range AllErrorCodes() {
			if decl == c {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("errorCatalog has entry %q with no AllErrorCodes constant", c)
		}
	}
}

func TestErrorCatalog_ExpectedSize(t *testing.T) {
	// error-handling.md §3 enumerates 20 codes across seven subsections.
	// Adding a new code requires updating both the spec and this assertion.
	const expectedCount = 20
	if got := len(AllErrorCodes()); got != expectedCount {
		t.Errorf("AllErrorCodes count = %d, want %d (error-handling.md §3)", got, expectedCount)
	}
}

func TestErrorCatalog_NoDuplicateCodes(t *testing.T) {
	seen := make(map[ErrorCode]struct{}, len(AllErrorCodes()))
	for _, c := range AllErrorCodes() {
		if _, dup := seen[c]; dup {
			t.Errorf("AllErrorCodes contains duplicate %q", c)
		}
		seen[c] = struct{}{}
	}
}

func TestErrorCatalog_NonEmptyDevMessage(t *testing.T) {
	for _, c := range AllErrorCodes() {
		spec := errorCatalog[c]
		if strings.TrimSpace(spec.devMessage) == "" {
			t.Errorf("ErrorCode %q has empty devMessage (every code must log something)", c)
		}
	}
}

func TestErrorCatalog_UserMessage_OrchestratorBoundContracts(t *testing.T) {
	// Codes that are published to the Orchestrator in
	// lic.events.status-changed.error_message MUST have a non-empty RU
	// UserMessage. Codes that are DLQ-only (inbound / IDEMPOTENCY_STORE)
	// may have an empty UserMessage by design.
	dlqOrInternalOnly := map[ErrorCode]struct{}{
		ErrCodeInvalidMessageSchema:    {},
		ErrCodeInvalidOrgIDMismatch:    {},
		ErrCodeIdempotencyStoreUnavail: {},
	}
	for _, c := range AllErrorCodes() {
		spec := errorCatalog[c]
		_, dlqOnly := dlqOrInternalOnly[c]
		switch {
		case dlqOnly && spec.userMessage != "":
			t.Errorf("ErrorCode %q is DLQ-only but has a UserMessage — should be empty", c)
		case !dlqOnly && spec.userMessage == "":
			t.Errorf("ErrorCode %q is published to Orchestrator but has empty UserMessage", c)
		}
	}
}

func TestErrorCatalog_RetryableFlagsMatchSpec(t *testing.T) {
	// Lock the Retryable column of error-handling.md §3 — drift would
	// silently change the Orchestrator's "повторить" UX or its DLQ logic.
	expected := map[ErrorCode]bool{
		ErrCodeInvalidMessageSchema:      false,
		ErrCodeInvalidOrgIDMismatch:      false,
		ErrCodeDMArtifactsTimeout:        true,
		ErrCodeDMArtifactsMissing:        true,
		ErrCodeDMPersistFailed:           true, // default; non-retryable callers override at call site
		ErrCodeDMPersistTimeout:          true,
		ErrCodeAgentOutputInvalid:        true,
		ErrCodeAgentTimeout:              true,
		ErrCodeAgentInputTooLarge:        false,
		ErrCodeAgentDependencyFailed:     false,
		ErrCodeLLMAllProvidersFailed:     true,
		ErrCodeLLMQuotaExceeded:          false,
		ErrCodeLLMContentPolicyViolation: false,
		ErrCodeAnalysisTimeout:           true,
		ErrCodeInputRejected:             false,
		ErrCodeDocumentTooLarge:          false,
		ErrCodeUserConfirmationExpired:   false,
		ErrCodeInvalidContractType:       false,
		ErrCodeInternal:                  true,
		ErrCodeIdempotencyStoreUnavail:   true,
	}
	if len(expected) != len(AllErrorCodes()) {
		t.Fatalf("expected-table size %d != AllErrorCodes count %d", len(expected), len(AllErrorCodes()))
	}
	for _, c := range AllErrorCodes() {
		want := expected[c]
		got := errorCatalog[c].retryable
		if got != want {
			t.Errorf("ErrorCode %q Retryable = %v, want %v (lock against error-handling.md §3)", c, got, want)
		}
	}
}

func TestErrorCatalog_UserMessageIsRussian(t *testing.T) {
	// Smoke check: when present, UserMessage must contain Cyrillic. The
	// orchestration layer forwards UserMessage verbatim — accidentally
	// translating one of them to English would corrupt the user-facing UX.
	for _, c := range AllErrorCodes() {
		spec := errorCatalog[c]
		if spec.userMessage == "" {
			continue
		}
		var hasCyrillic bool
		for _, r := range spec.userMessage {
			if unicode.Is(unicode.Cyrillic, r) {
				hasCyrillic = true
				break
			}
		}
		if !hasCyrillic {
			t.Errorf("ErrorCode %q UserMessage = %q — must contain Cyrillic (RU localisation)", c, spec.userMessage)
		}
	}
}

func TestLookupErrorSpec(t *testing.T) {
	spec, ok := LookupErrorSpec(ErrCodeAnalysisTimeout)
	if !ok {
		t.Fatal("LookupErrorSpec must succeed for a registered code")
	}
	if !spec.Retryable {
		t.Error("ANALYSIS_TIMEOUT must be retryable")
	}
	if spec.UserMessage == "" {
		t.Error("UserMessage must be populated")
	}
	if spec.DevMessage == "" {
		t.Error("DevMessage must be populated")
	}

	if _, ok := LookupErrorSpec(ErrorCode("UNREGISTERED")); ok {
		t.Error("LookupErrorSpec must return ok=false for an unknown code")
	}

	// Zero ErrorSpec for unknown — no partial leak.
	zero, _ := LookupErrorSpec(ErrorCode("UNREGISTERED"))
	if zero != (ErrorSpec{}) {
		t.Errorf("LookupErrorSpec on unknown code must return zero ErrorSpec, got %+v", zero)
	}
}

func TestErrorCode_IsPublishableToOrchestrator(t *testing.T) {
	cases := []struct {
		code      ErrorCode
		wantValue bool
	}{
		// Published (non-empty RU UserMessage).
		{ErrCodeAnalysisTimeout, true},
		{ErrCodeDMArtifactsTimeout, true},
		{ErrCodeAgentOutputInvalid, true},
		{ErrCodeInvalidContractType, true},
		// DLQ-only (empty UserMessage).
		{ErrCodeInvalidMessageSchema, false},
		{ErrCodeInvalidOrgIDMismatch, false},
		{ErrCodeIdempotencyStoreUnavail, false},
		// Unknown — must default to false (safe).
		{ErrorCode("UNREGISTERED"), false},
	}
	for _, tc := range cases {
		if got := tc.code.IsPublishableToOrchestrator(); got != tc.wantValue {
			t.Errorf("ErrorCode(%q).IsPublishableToOrchestrator() = %v, want %v", tc.code, got, tc.wantValue)
		}
	}
}

func TestErrorCatalog_WireStringsAreUnique(t *testing.T) {
	// Defends against a typo where two constants share the same wire
	// string — TestErrorCatalog_NoDuplicateCodes covers identity-level
	// duplicates but not different constants pointing at the same string.
	seen := make(map[string]ErrorCode, len(AllErrorCodes()))
	for _, c := range AllErrorCodes() {
		s := string(c)
		if prev, dup := seen[s]; dup {
			t.Errorf("wire-format collision: constants %q and %q both map to string %q", prev, c, s)
		}
		seen[s] = c
	}
}

func TestErrorCode_WireFormat_LockedConstants(t *testing.T) {
	// These literals are emitted into lic.events.status-changed.error_code
	// and DLQ envelopes. Lock them against accidental rename.
	pairs := []struct {
		code ErrorCode
		wire string
	}{
		{ErrCodeAnalysisTimeout, "ANALYSIS_TIMEOUT"},
		{ErrCodeDMArtifactsTimeout, "DM_ARTIFACTS_TIMEOUT"},
		{ErrCodeAgentOutputInvalid, "AGENT_OUTPUT_INVALID"},
		{ErrCodeLLMAllProvidersFailed, "LLM_ALL_PROVIDERS_FAILED"},
		{ErrCodeUserConfirmationExpired, "USER_CONFIRMATION_EXPIRED"},
		{ErrCodeInvalidContractType, "INVALID_CONTRACT_TYPE"},
		{ErrCodeDocumentTooLarge, "DOCUMENT_TOO_LARGE"},
		{ErrCodeInternal, "INTERNAL_ERROR"},
	}
	for _, p := range pairs {
		if string(p.code) != p.wire {
			t.Errorf("ErrorCode constant drift: got %q, want %q", string(p.code), p.wire)
		}
	}
}
